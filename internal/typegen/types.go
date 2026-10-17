package typegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// builtinTypes maps Go's predeclared types onto TypeScript. Go's integer types
// all become `number` because that is what they are over JSON; the precision
// caveat for int64 above 2^53 is real and is documented rather than papered over
// with a string conversion.
var builtinTypes = map[string]string{
	"string": "string",
	"bool":   "boolean",

	"int":     "number",
	"int8":    "number",
	"int16":   "number",
	"int32":   "number",
	"int64":   "number",
	"uint":    "number",
	"uint8":   "number",
	"uint16":  "number",
	"uint32":  "number",
	"uint64":  "number",
	"uintptr": "number",

	"float32": "number",
	"float64": "number",

	"byte": "number",
	"rune": "number",

	"error": "string",
}

// qualifiedTypes maps well-known types from other packages. Anything absent falls
// back to `unknown` with a warning rather than guessing.
var qualifiedTypes = map[string]string{
	"time.Time":          "string", // RFC 3339 over JSON
	"time.Duration":      "number", // nanoseconds
	"uuid.UUID":          "string",
	"json.RawMessage":    "unknown",
	"json.Number":        "number",
	"decimal.Decimal":    "string",
	"gorm.DeletedAt":     "string | null",
	"orm.SoftDelete":     "string | null",
	"vuepher.SoftDelete": "string | null",
}

// undefinedPrefix marks a conversion whose value may be absent. buildStruct turns
// it into an optional property.
const undefinedPrefix = "undefined | "

// generatedTypes records the type names a run emits, grouped by the package name
// they are declared in. It is what lets a field referencing `models.Post` become
// a reference to the generated `Post` interface instead of `unknown`.
//
// The lookup is by package name as written in the source (the import qualifier)
// and only succeeds for types this run actually generates, so a reference can
// never end up pointing at a declaration that does not exist.
type generatedTypes map[string]map[string]bool

// has reports whether pkg.name is a type this run generates.
func (g generatedTypes) has(pkg, name string) bool {
	names, ok := g[pkg]
	return ok && names[name]
}

// ConvertExpr converts a Go type expression to a TypeScript type. The second
// return value is a warning when the conversion was approximate; the caller
// collects those so a limitation is discoverable rather than silent (R3).
//
// named holds the types declared in the package being converted; generated holds
// the types every scanned package produces.
func ConvertExpr(expr ast.Expr, named map[string]ast.Expr, generated generatedTypes) (string, string) {
	switch node := expr.(type) {
	case *ast.Ident:
		return convertIdent(node.Name, named)

	case *ast.StarExpr:
		// A pointer is nullable in JSON: the key is present with a null value.
		inner, warning := ConvertExpr(node.X, named, generated)
		// `unknown | null` says nothing that `unknown` does not, and a union of the
		// same type twice reads as noise in the generated file.
		if inner == "unknown" || strings.HasSuffix(inner, "| null") {
			return inner, warning
		}
		return inner + " | null", warning

	case *ast.SelectorExpr:
		full := qualifiedName(node)
		if mapped, ok := qualifiedTypes[full]; ok {
			return mapped, ""
		}
		// A type another scanned package declares: reference the generated
		// interface. This is the case that matters, because props structs refer to
		// models as models.Post.
		if qualifier := qualifierName(node); qualifier != "" && generated.has(qualifier, node.Sel.Name) {
			return node.Sel.Name, ""
		}
		if isProbablyEnum(full) {
			return "unknown", fmt.Sprintf(
				"%s looks like an enum in a package that was not scanned; add that directory to typegen sources to get a union type",
				full,
			)
		}
		return "unknown", fmt.Sprintf("%s is not a known type; generated as unknown.", full)

	case *ast.IndexExpr, *ast.IndexListExpr:
		// A generic instantiation such as orm.Paginator[Post]. The generator has
		// no generic declarations to reference, so the shape is not knowable.
		return "unknown", fmt.Sprintf(
			"%s is a generic type; generated as unknown. Declare a non-generic props struct to get it typed",
			describeExpr(expr),
		)

	case *ast.ArrayType:
		if isByteSlice(node) {
			// encoding/json renders []byte as a base64 string.
			return "string", ""
		}
		element, warning := ConvertExpr(node.Elt, named, generated)
		return element + "[]", warning

	case *ast.MapType:
		value, warning := ConvertExpr(node.Value, named, generated)
		if warning == "" {
			if keyName, ok := node.Key.(*ast.Ident); !ok || keyName.Name != "string" {
				warning = "map keys are not strings; JSON object keys are always strings"
			}
		}
		return "Record<string, " + value + ">", warning

	case *ast.InterfaceType:
		// any / interface{}: the shape is not declared anywhere.
		return "unknown", ""

	case *ast.StructType:
		return "unknown", "an anonymous struct has no name; declare a named type to get a typed declaration"

	case *ast.FuncType:
		return "unknown", "functions cannot be serialised; exclude this field with json:\"-\""

	case *ast.ChanType:
		return "unknown", "channels cannot be serialised"

	case *ast.Ellipsis:
		element, warning := ConvertExpr(node.Elt, named, generated)
		return element + "[]", warning

	case *ast.ParenExpr:
		return ConvertExpr(node.X, named, generated)

	default:
		return "unknown", fmt.Sprintf("%s is a %s, which cannot be serialised", describeExpr(expr), kindOf(expr))
	}
}

// qualifierName returns the package name a selector is qualified with, or "".
func qualifierName(node *ast.SelectorExpr) string {
	ident, ok := node.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// describeExpr renders an expression as the Go source a reader would recognise,
// so a warning names `orm.Paginator[Post]` rather than `*ast.IndexExpr`.
func describeExpr(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.IndexExpr:
		return describeExpr(node.X) + "[" + describeExpr(node.Index) + "]"
	case *ast.IndexListExpr:
		parts := make([]string, 0, len(node.Indices))
		for _, index := range node.Indices {
			parts = append(parts, describeExpr(index))
		}
		return describeExpr(node.X) + "[" + strings.Join(parts, ", ") + "]"
	case *ast.StarExpr:
		return "*" + describeExpr(node.X)
	case *ast.ArrayType:
		return "[]" + describeExpr(node.Elt)
	}
	if name := qualifiedName(expr); name != "" {
		return name
	}
	return "a Go type"
}

// kindOf names the Go kind of an expression for a warning message.
func kindOf(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return "generic type"
	case *ast.SelectorExpr, *ast.Ident:
		return "type"
	case *ast.StarExpr:
		return "pointer"
	case *ast.ArrayType:
		return "slice"
	default:
		return "type"
	}
}

// convertIdent maps an identifier, resolving named types declared in the same
// file.
func convertIdent(name string, named map[string]ast.Expr) (string, string) {
	if mapped, ok := builtinTypes[name]; ok {
		return mapped, ""
	}

	if underlying, ok := named[name]; ok {
		switch inner := underlying.(type) {
		case *ast.StructType:
			// A named struct declared in this file: reference it.
			return name, ""
		case *ast.InterfaceType:
			return "unknown", ""
		case *ast.Ident:
			if mapped, ok := builtinTypes[inner.Name]; ok {
				// A named type over a primitive, e.g. `type Status string`.
				// Emitting the alias keeps the meaning, but the alias has to be
				// exported in the generated file too, so resolve to the base.
				return mapped, ""
			}
		}
		return "unknown", fmt.Sprintf("%s is a named type with no generated declaration", name)
	}

	// Not a predeclared type and not declared here: it comes from another
	// package the generator did not read.
	return "unknown", fmt.Sprintf("%s is not declared in the scanned files", name)
}

// isByteSlice reports whether an array type is []byte or []uint8.
func isByteSlice(node *ast.ArrayType) bool {
	if node.Len != nil {
		return false
	}
	ident, ok := node.Elt.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "byte" || ident.Name == "uint8"
}

// isProbablyEnum reports whether a qualified name looks like a constant defined
// with a typed const block, which is emitted as a union of literals. Without
// type resolution this is a heuristic, so it only accepts names that are all
// upper case or end in a conventional suffix.
func isProbablyEnum(name string) bool {
	segments := strings.Split(name, ".")
	if len(segments) != 2 {
		return false
	}
	typeName := segments[1]
	if typeName == "" {
		return false
	}
	return typeName == strings.ToUpper(typeName) || strings.HasSuffix(typeName, "Status") ||
		strings.HasSuffix(typeName, "Kind") || strings.HasSuffix(typeName, "Type")
}

// ConvertToken maps a Go token kind onto a TypeScript literal type, used by the
// enum builder.
func ConvertToken(kind token.Token) string {
	switch kind {
	case token.INT:
		return "number"
	case token.FLOAT:
		return "number"
	case token.STRING:
		return "string"
	case token.CHAR:
		return "string"
	default:
		return "unknown"
	}
}
