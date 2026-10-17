package procman

import (
	"bufio"
	"io"
)

// copyLines forwards a stream line by line to a writer. Line-at-a-time output is
// what lets the prefix writer label every line and keep two processes' output
// from interleaving.
func copyLines(reader io.Reader, writer io.Writer) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		_, _ = writer.Write(append(scanner.Bytes(), '\n'))
	}
}
