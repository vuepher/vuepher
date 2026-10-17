package support

import (
	"io/fs"

	"github.com/vuepher/vuepher/stubs"
)

// StubFS is the embedded filesystem holding the generator templates and the
// application skeleton. Every file inside is named `*.stub`; the generator
// strips the suffix when it writes the output.
//
// A project can override any stub by creating a file at the same relative path
// under `stubs/` in the project root (`vuepher stub:publish` copies them all).
// Resolution order is the project's stubs directory first, then this embedded
// set, which is what makes `stubs:overscroll` unnecessary: the same mechanism
// covers a project that wants to change one controller template and nothing
// else.
var StubFS fs.FS = stubs.FS

// StubSuffix is the extension appended to every stub file so that the templates
// are not compiled as part of the module. `main.go.stub` is data, `main.go`
// would be a package.
const StubSuffix = ".stub"

// ProjectStubDir is the directory inside a project that overrides embedded
// stubs.
const ProjectStubDir = "stubs"

// FrameworkModule is the import path of the framework. It appears in generated
// go.mod files, imports and the replace directive used during local
// development.
const FrameworkModule = "github.com/vuepher/vuepher"
