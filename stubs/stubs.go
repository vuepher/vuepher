// Package stubs embeds the generator templates and the application skeleton into
// the CLI binary.
//
// Every template is stored with a `.stub` suffix. Two reasons: a file named
// `main.go` inside a subdirectory would be compiled as a package of this module,
// and the suffix makes it obvious in an editor that the file is a template
// rather than working code. The generator strips the suffix when writing the
// output.
//
// Templates use `[[` and `]]` as their delimiters rather than the default
// `{{`/`}}`, because Vue templates use the default pair for interpolation and
// single-file components are among the things generated.
package stubs

import "embed"

// FS holds every stub. The directories are listed explicitly; go:embed has no
// recursive `**` pattern, and embedding the parent directory would pull this
// package's own source in as data.
//
//go:embed all:app all:controller all:model all:middleware all:view all:request all:migration all:seeder all:factory
var FS embed.FS
