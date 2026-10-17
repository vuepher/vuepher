// Package support holds small helpers shared between the framework, the CLI and
// the generators: naming conventions, path utilities and the embedded stub
// filesystem.
//
// It deliberately has no dependency on the core framework package, so the CLI
// can use it without pulling the whole HTTP stack into its binary.
package support

import (
	"strings"
	"time"
	"unicode"

	"github.com/jinzhu/inflection"
)

// ---------------------------------------------------------------------------
// Case conversion
// ---------------------------------------------------------------------------

// Studly converts a name to UpperCamelCase (Laravel's "StudlyCase"):
//
//	blog_post  -> BlogPost
//	blog-post  -> BlogPost
//	blogPost   -> BlogPost
//	BlogPost   -> BlogPost
func Studly(value string) string {
	words := splitWords(value)
	var b strings.Builder
	for _, word := range words {
		b.WriteString(upperFirst(strings.ToLower(word)))
	}
	return b.String()
}

// Camel converts a name to lowerCamelCase:
//
//	BlogPost -> blogPost
func Camel(value string) string {
	studly := Studly(value)
	if studly == "" {
		return ""
	}
	return lowerFirst(studly)
}

// Snake converts a name to snake_case:
//
//	BlogPost -> blog_post
//	BlogPOST -> blog_post
func Snake(value string) string {
	words := splitWords(value)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	return strings.Join(words, "_")
}

// Kebab converts a name to kebab-case, used for route paths and generated CSS:
//
//	BlogPost -> blog-post
func Kebab(value string) string {
	return strings.ReplaceAll(Snake(value), "_", "-")
}

// Title converts a name to a space-separated, human-readable label:
//
//	blog_post -> Blog post
func Title(value string) string {
	words := splitWords(value)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	text := strings.Join(words, " ")
	if text == "" {
		return ""
	}
	return upperFirst(text)
}

// PackageName returns a valid Go package name for a directory or type name.
// Go keywords are suffixed so that a folder called `type` still compiles.
func PackageName(value string) string {
	name := strings.ToLower(strings.ReplaceAll(Snake(value), "_", ""))
	if name == "" {
		return "app"
	}
	if _, reserved := goKeywords[name]; reserved {
		return name + "pkg"
	}
	if unicode.IsDigit(rune(name[0])) {
		return "p" + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Pluralisation
// ---------------------------------------------------------------------------

// Plural pluralises a snake_case or CamelCase name, preserving the case style:
//
//	post      -> posts
//	BlogPost  -> BlogPosts
//	category  -> categories
func Plural(value string) string {
	return preserveCase(value, inflection.Plural)
}

// Singular singularises a name, preserving case style:
//
//	posts    -> post
//	BlogPosts -> BlogPost
func Singular(value string) string {
	return preserveCase(value, inflection.Singular)
}

// TableName returns the database table name for a model name, applying
// Snake + Plural:
//
//	Post         -> posts
//	BlogPost     -> blog_posts
//	Category     -> categories
//
// It matches the naming strategy the ORM uses (see orm.TableNameOf), which is
// the property that keeps generated migrations and generated models in sync.
func TableName(model string) string {
	return Plural(Snake(model))
}

// ResourceName returns the route resource name for a controller or model name:
//
//	PostController -> posts
//	BlogPost       -> blog_posts
func ResourceName(value string) string {
	base := strings.TrimSuffix(value, "Controller")
	return TableName(strings.TrimSuffix(base, "Controller"))
}

// ControllerName ensures a name ends with "Controller".
func ControllerName(value string) string {
	base := Studly(value)
	if base == "" {
		return "Controller"
	}
	if strings.HasSuffix(base, "Controller") {
		return base
	}
	return base + "Controller"
}

// modelSuffixes are the artifact suffixes stripped when deriving the model an
// artifact pairs with. `make:controller PostController`, `make:factory
// PostFactory` and `make:seeder PostSeeder` all refer to the Post model.
var modelSuffixes = []string{
	"Controller",
	"Factory",
	"Seeder",
	"Request",
	"Middleware",
}

// ModelName strips the generator suffix so the paired model can be derived:
//
//	PostController -> Post
//	PostFactory    -> Post
//
// A suffix is only stripped when something is left over, so `make:model Factory`
// still names a model rather than producing an empty name.
func ModelName(value string) string {
	base := Studly(value)
	for _, suffix := range modelSuffixes {
		trimmed := strings.TrimSuffix(base, suffix)
		if trimmed != "" && trimmed != base {
			return trimmed
		}
	}
	return base
}

// MigrationName keeps a migration name snake_case and without a timestamp, so it
// can be prefixed with one separately.
func MigrationName(value string) string {
	name := Snake(value)
	name = strings.Trim(name, "_")
	if name == "" {
		name = "migration"
	}
	// A leading digit would make an invalid Go identifier once prefixed.
	if unicode.IsDigit(rune(name[0])) {
		name = "m" + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Migration timestamps
// ---------------------------------------------------------------------------

// MigrationTimestampFormat is the layout used for migration file prefixes. It
// sorts lexicographically in chronological order, which is what lets the
// migrator run files in the right sequence without reading them.
const MigrationTimestampFormat = "2006_01_02_150405"

// MigrationTimestamp renders the timestamp prefix for a migration file.
func MigrationTimestamp(at time.Time) string {
	return at.UTC().Format(MigrationTimestampFormat)
}

// timestampRegexp is documented here rather than compiled, because the CLI only
// needs to strip a prefix. Kept as a note so the format has one description:
// ^[0-9]{4}_[0-9]{2}_[0-9]{2}_[0-9]{6}_

// StripTimestamp removes a leading migration timestamp from a file name.
func StripTimestamp(name string) string {
	const prefixLength = 17 // 2006_01_02_150405
	if len(name) <= prefixLength {
		return name
	}
	if name[4] != '_' || name[7] != '_' || name[10] != '_' {
		return name
	}
	for _, index := range []int{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 13, 14, 15, 16} {
		if name[index] < '0' || name[index] > '9' {
			return name
		}
	}
	return strings.TrimPrefix(name[prefixLength:], "_")
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

// splitWords breaks an identifier into its words, handling snake_case,
// kebab-case, spaces, acronym runs and digits:
//
//	BlogPost    -> [Blog Post]
//	blog_post   -> [blog post]
//	APIKey      -> [API Key]
//	user2Factor -> [user2 Factor]
func splitWords(value string) []string {
	var words []string
	var current []rune
	runes := []rune(strings.TrimSpace(value))

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.' || r == '/':
			if len(current) > 0 {
				words = append(words, string(current))
				current = nil
			}
		case unicode.IsUpper(r):
			// Start a new word at a lower-to-upper transition, or at the end of
			// an acronym run followed by a lowercase letter (APIKey -> API Key).
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			acronymEnd := i > 0 && unicode.IsUpper(runes[i-1]) &&
				i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if len(current) > 0 && (prevLower || acronymEnd) {
				words = append(words, string(current))
				current = nil
			}
			current = append(current, r)
		default:
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

// preserveCase applies a word transform while keeping the original case style.
func preserveCase(value string, transform func(string) string) string {
	if value == "" {
		return ""
	}
	style := detectStyle(value)
	words := splitWords(value)
	if len(words) == 0 {
		return value
	}

	// Only the final word changes number: BlogPost -> BlogPosts, not BlogsPosts.
	words[len(words)-1] = transform(words[len(words)-1])

	switch style {
	case styleCamel, styleStudly:
		var b strings.Builder
		for _, word := range words {
			b.WriteString(upperFirst(word))
		}
		out := b.String()
		if style == styleCamel {
			return lowerFirst(out)
		}
		return out
	case styleKebab:
		for i, word := range words {
			words[i] = strings.ToLower(word)
		}
		return strings.Join(words, "-")
	default:
		for i, word := range words {
			words[i] = strings.ToLower(word)
		}
		return strings.Join(words, "_")
	}
}

type nameStyle int

const (
	styleSnake nameStyle = iota
	styleKebab
	styleStudly
	styleCamel
)

func detectStyle(value string) nameStyle {
	switch {
	case strings.Contains(value, "_"):
		return styleSnake
	case strings.Contains(value, "-"):
		return styleKebab
	case len(value) > 0 && unicode.IsUpper(rune(value[0])):
		return styleStudly
	default:
		return styleCamel
	}
}

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	// Leave an acronym alone: APIClient stays APIClient, not aPIClient.
	if len(runes) > 1 && unicode.IsUpper(runes[0]) && unicode.IsUpper(runes[1]) {
		return value
	}
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// goKeywords are reserved words that cannot be package names.
var goKeywords = map[string]struct{}{
	"break": {}, "case": {}, "chan": {}, "const": {}, "continue": {}, "default": {},
	"defer": {}, "else": {}, "fallthrough": {}, "for": {}, "func": {}, "go": {},
	"goto": {}, "if": {}, "import": {}, "interface": {}, "map": {}, "package": {},
	"range": {}, "return": {}, "select": {}, "struct": {}, "switch": {}, "type": {},
	"var": {},
}
