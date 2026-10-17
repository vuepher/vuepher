package cli

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/andybalholm/brotli"
)

// TestCompressFileWritesUsableVariants guards the bug that shipped empty .br
// files: the brotli writer buffers its final block, so a writer that is never
// closed produces zero bytes. The asset handler prefers the .br sibling for every
// browser that accepts brotli, so an empty variant means the built application
// serves empty JavaScript to every modern browser while the build reports
// success.
func TestCompressFileWritesUsableVariants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app-abc123.js")
	original := bytes.Repeat([]byte("export function noop() { return 1 }\n"), 200)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("could not write the fixture: %v", err)
	}

	if err := compressFile(path); err != nil {
		t.Fatalf("compressFile failed: %v", err)
	}

	brotliBytes := readFile(t, path+".br")
	if len(brotliBytes) == 0 {
		t.Fatal("the .br variant is empty: a browser would receive no asset at all")
	}
	if len(brotliBytes) >= len(original) {
		t.Errorf("the .br variant is %d bytes for %d bytes of input", len(brotliBytes), len(original))
	}
	if got := decodeBrotli(t, brotliBytes); !bytes.Equal(got, original) {
		t.Error("the .br variant does not decompress back to the original asset")
	}

	gzipBytes := readFile(t, path+".gz")
	if len(gzipBytes) == 0 {
		t.Fatal("the .gz variant is empty")
	}
	if got := decodeGzip(t, gzipBytes); !bytes.Equal(got, original) {
		t.Error("the .gz variant does not decompress back to the original asset")
	}
}

// TestCompressFileSkipsSmallAssets keeps the threshold honest: compressing a tiny
// file costs more than it saves, and the handler falls back to the original.
func TestCompressFileSkipsSmallAssets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.css")
	if err := os.WriteFile(path, []byte(".a{color:red}"), 0o644); err != nil {
		t.Fatalf("could not write the fixture: %v", err)
	}

	if err := compressFile(path); err != nil {
		t.Fatalf("compressFile failed: %v", err)
	}
	for _, suffix := range []string{".br", ".gz"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Errorf("%s was written for an asset below the compression threshold", suffix)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	return data
}

func decodeBrotli(t *testing.T, data []byte) []byte {
	t.Helper()
	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("the .br variant is not valid brotli: %v", err)
	}
	return decoded
}

func decodeGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the .gz variant is not valid gzip: %v", err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("the .gz variant could not be decompressed: %v", err)
	}
	return decoded
}
