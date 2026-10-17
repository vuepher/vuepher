package cli

import (
	"fmt"
	"os"
	"strings"
)

// ANSI codes. Colour is suppressed when the output is not a terminal or NO_COLOR
// is set, so a CI log stays readable.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

var colorEnabled = detectColor()

func detectColor() bool {
	if value, ok := os.LookupEnv("NO_COLOR"); ok && value != "" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func paint(code, text string) string {
	if !colorEnabled {
		return text
	}
	return code + text + ansiReset
}

// styleSuccess marks a completed action.
func styleSuccess(text string) string { return paint(ansiGreen, text) }

// styleError marks a failure.
func styleError(text string) string { return paint(ansiRed, text) }

// styleWarn marks something the user should look at.
func styleWarn(text string) string { return paint(ansiYellow, text) }

// styleInfo marks a path or identifier in output.
func styleInfo(text string) string { return paint(ansiCyan, text) }

// styleHeading marks a section heading.
func styleHeading(text string) string { return paint(ansiBold, text) }

// styleMuted marks secondary detail.
func styleMuted(text string) string { return paint(ansiDim, text) }

// printf writes formatted output to stdout.
func printf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, format, args...)
}

// println writes a line to stdout.
func println(args ...any) {
	fmt.Fprintln(os.Stdout, args...)
}

// printCreated reports a file that a generator wrote, in the form
// "created  app/models/post.go".
func printCreated(path string) {
	printf("  %s %s\n", styleSuccess("created"), styleInfo(path))
}

// printSkipped reports a file a generator left alone.
func printSkipped(path, reason string) {
	printf("  %s %s %s\n", styleWarn("skipped"), styleInfo(path), styleMuted(reason))
}

// printWarning writes a warning to stderr.
func printWarning(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleWarn("warning:"), fmt.Sprintf(format, args...))
}

// printStep writes a progress line.
func printStep(format string, args ...any) {
	printf("%s %s\n", styleMuted("•"), fmt.Sprintf(format, args...))
}

// printBullet writes a list item, for doctor and route:list output.
func printBullet(label, value string) {
	printf("  %-24s %s\n", label, value)
}
