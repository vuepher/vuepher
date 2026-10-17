package procman

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// ANSI escape codes. Kept explicit rather than importing a colour library: the
// framework needs five sequences and a reset.
const (
	reset   = "\033[0m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
)

var colorCodes = map[string]string{
	"black":   "\033[30m",
	"red":     red,
	"green":   green,
	"yellow":  yellow,
	"blue":    blue,
	"magenta": magenta,
	"cyan":    cyan,
	"white":   "\033[37m",
}

// prefixWriter tags every line with the process it came from.
//
// Output is written under a shared lock so that two processes writing at the same
// moment cannot interleave inside a line — the failure mode looks like corrupted
// logs and is confusing to debug.
type prefixWriter struct {
	out    io.Writer
	prefix string
	color  string
	mu     *sync.Mutex

	// width pads the prefix so columns line up regardless of process name length.
	width int
}

func newPrefixWriter(out io.Writer, name, color string, mu *sync.Mutex) io.Writer {
	return &prefixWriter{
		out:    out,
		prefix: name,
		color:  color,
		mu:     mu,
		width:  8,
	}
}

// Write implements io.Writer. Lines are written individually so the prefix is
// reproduced for each one.
func (w *prefixWriter) Write(data []byte) (int, error) {
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}

	text := string(data)
	trailingNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		if _, err := fmt.Fprintf(w.out, "%s%s>%s %s%s\n",
			w.colorCode(), pad(w.prefix, w.width), reset, line, reset); err != nil {
			return 0, err
		}
	}
	if trailingNewline {
		// The newline has already been emitted per line.
		return len(data), nil
	}
	return len(data), nil
}

// colorCode resolves the configured colour, honouring NO_COLOR.
func (w *prefixWriter) colorCode() string {
	if noColor() {
		return ""
	}
	if code, ok := colorCodes[w.color]; ok {
		return code
	}
	// Fall back to dim so a role without a colour is still distinguishable.
	return dim
}

// pad right-pads a label to a fixed width.
func pad(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}
