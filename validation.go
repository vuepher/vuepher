package vuepher

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ErrorBag is the set of validation failures for one form, keyed by field name.
// It is what the frontend receives as the `errors` prop (VIEW-08).
//
// A field maps to a single message — the first rule that failed — because that
// is what a form shows under an input. Rules that need the full list (password
// complexity) should express it as a single custom rule instead.
type ErrorBag map[string]string

// Any reports whether the bag contains at least one failure.
func (b ErrorBag) Any() bool { return len(b) > 0 }

// Get returns the message for a field, or "" when the field is valid.
func (b ErrorBag) Get(field string) string { return b[field] }

// Add records a failure.
func (b ErrorBag) Add(field, message string) {
	if b == nil {
		return
	}
	b[field] = message
}

// Merge copies another bag into this one, without overwriting existing entries.
func (b ErrorBag) Merge(other ErrorBag) {
	for field, message := range other {
		if _, exists := b[field]; exists {
			continue
		}
		b[field] = message
	}
}

// Messages returns every message, sorted by field name for stable output.
func (b ErrorBag) Messages() []string {
	keys := b.Keys()
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, b[key])
	}
	return out
}

// Keys returns the failing field names in sorted order.
func (b ErrorBag) Keys() []string {
	keys := make([]string, 0, len(b))
	for key := range b {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Messages and localisation
// ---------------------------------------------------------------------------

// defaultMessages holds the built-in English messages, keyed by validation rule.
// {field} is replaced with the humanised field name and {param} with the rule
// parameter (for example the 3 in `min=3`).
var defaultMessages = map[string]string{
	"required":  "The {field} field is required.",
	"email":     "The {field} field must be a valid email address.",
	"min":       "The {field} field must be at least {param} characters.",
	"max":       "The {field} field must not be greater than {param} characters.",
	"len":       "The {field} field must be exactly {param} characters.",
	"gte":       "The {field} field must be greater than or equal to {param}.",
	"lte":       "The {field} field must be less than or equal to {param}.",
	"gt":        "The {field} field must be greater than {param}.",
	"lt":        "The {field} field must be less than {param}.",
	"oneof":     "The selected {field} is invalid.",
	"url":       "The {field} field must be a valid URL.",
	"uuid":      "The {field} field must be a valid UUID.",
	"numeric":   "The {field} field must be a number.",
	"alphanum":  "The {field} field may only contain letters and numbers.",
	"eqfield":   "The {field} field must match {param}.",
	"datetime":  "The {field} field must be a valid date.",
	"boolean":   "The {field} field must be true or false.",
	"unique":    "The {field} has already been taken.",
	"confirmed": "The {field} confirmation does not match.",
	"default":   "The {field} field is invalid.",
}

// indonesianMessages is the Bahasa Indonesia translation shipped with the
// framework (PRD §7.3).
var indonesianMessages = map[string]string{
	"required":  "Kolom {field} wajib diisi.",
	"email":     "Kolom {field} harus berupa alamat email yang valid.",
	"min":       "Kolom {field} minimal {param} karakter.",
	"max":       "Kolom {field} maksimal {param} karakter.",
	"len":       "Kolom {field} harus tepat {param} karakter.",
	"gte":       "Kolom {field} harus lebih besar atau sama dengan {param}.",
	"lte":       "Kolom {field} harus lebih kecil atau sama dengan {param}.",
	"oneof":     "Pilihan {field} tidak valid.",
	"url":       "Kolom {field} harus berupa URL yang valid.",
	"uuid":      "Kolom {field} harus berupa UUID yang valid.",
	"numeric":   "Kolom {field} harus berupa angka.",
	"eqfield":   "Kolom {field} harus sama dengan {param}.",
	"unique":    "Nilai {field} sudah digunakan.",
	"confirmed": "Konfirmasi {field} tidak cocok.",
	"default":   "Kolom {field} tidak valid.",
}

// Translator resolves messages for one locale. Applications can register extra
// locales or override individual rules.
type Translator struct {
	locales  map[string]map[string]string
	fallback string
	current  string
}

// NewTranslator builds a translator seeded with the built-in English and
// Indonesian messages. locale selects the active language.
func NewTranslator(locale string) *Translator {
	if locale == "" {
		locale = "en"
	}
	return &Translator{
		locales: map[string]map[string]string{
			"en": cloneMessages(defaultMessages),
			"id": cloneMessages(indonesianMessages),
		},
		fallback: "en",
		current:  locale,
	}
}

// SetLocale switches the active language.
func (t *Translator) SetLocale(locale string) { t.current = locale }

// Locale reports the active language.
func (t *Translator) Locale() string { return t.current }

// Extend adds or overrides messages for a locale.
func (t *Translator) Extend(locale string, messages map[string]string) {
	if t.locales == nil {
		t.locales = map[string]map[string]string{}
	}
	if t.locales[locale] == nil {
		t.locales[locale] = map[string]string{}
	}
	for rule, message := range messages {
		t.locales[locale][rule] = message
	}
}

// Message renders the message for a rule in the active locale.
func (t *Translator) Message(rule, field, param string) string {
	text := t.lookup(t.current, rule)
	if text == "" {
		text = t.lookup(t.fallback, rule)
	}
	if text == "" {
		text = defaultMessages["default"]
	}
	text = strings.ReplaceAll(text, "{field}", humanizeField(field))
	text = strings.ReplaceAll(text, "{param}", param)
	return text
}

func (t *Translator) lookup(locale, rule string) string {
	messages, ok := t.locales[locale]
	if !ok {
		return ""
	}
	return messages[rule]
}

func cloneMessages(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Validator
// ---------------------------------------------------------------------------

// Validator validates request structs against their `validate` tags (HTTP-07):
//
//	type StorePost struct {
//		Title string `json:"title" validate:"required,min=3,max=120"`
//		Body  string `json:"body"  validate:"required"`
//	}
//
// Rules come from go-playground/validator, which Vuepher wraps so that messages,
// field naming and the ErrorBag shape are consistent across the framework.
type Validator struct {
	Engine     *validator.Validate
	Translator *Translator

	// Rules holds custom rule names registered through Register, used to look up
	// the message when a custom rule fails.
	rules map[string]struct{}
}

// NewValidator builds a validator with the framework's default messages.
func NewValidator(locale string) *Validator {
	engine := validator.New(validator.WithRequiredStructEnabled())

	// Report fields using their JSON name so that messages line up with the
	// props the frontend receives.
	engine.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return field.Name
		}
		return name
	})

	return &Validator{
		Engine:     engine,
		Translator: NewTranslator(locale),
		rules:      map[string]struct{}{},
	}
}

// Register adds a custom validation rule under the given name. Registering a
// rule does not write a message for it; call Translator.Extend for that, or pass
// one here.
//
// TODO(phase-1): replace the free-form signature with a typed Rule interface so
// that rules can declare whether they need extra arguments.
func (v *Validator) Register(name string, fn validator.Func, message string) error {
	if err := v.Engine.RegisterValidation(name, fn); err != nil {
		return fmt.Errorf("vuepher: could not register validation rule %q: %w", name, err)
	}
	v.rules[name] = struct{}{}
	if message != "" {
		v.Translator.Extend(v.Translator.Locale(), map[string]string{name: message})
	}
	return nil
}

// Validate checks dst and returns the failures. The returned bag is nil when
// validation passed, so it compares cleanly against nil.
func (v *Validator) Validate(dst any) ErrorBag {
	if dst == nil {
		return nil
	}
	err := v.Engine.Struct(dst)
	if err == nil {
		return nil
	}

	var invalid *validator.InvalidValidationError
	if ok := errorAs(err, &invalid); ok {
		// A programming error (validation on a non-struct), not user input.
		bag := ErrorBag{}
		bag.Add("_request", fmt.Sprintf("vuepher: validation target is not a struct: %v", invalid))
		return bag
	}

	bag := ErrorBag{}
	fieldErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		bag.Add("_request", "The submitted data is invalid.")
		return bag
	}
	for _, fieldErr := range fieldErrors {
		field := fieldErr.Field()
		if _, exists := bag[field]; exists {
			continue
		}
		bag[field] = v.Translator.Message(fieldErr.Tag(), field, fieldErr.Param())
	}
	return bag
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// humanizeField turns a field name into something readable in a sentence:
// "user_name" and "UserName" both become "User name".
func humanizeField(field string) string {
	if field == "" {
		return "field"
	}
	var spaced strings.Builder
	runes := []rune(field)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			spaced.WriteRune(' ')
		case i > 0 && r >= 'A' && r <= 'Z' && !(runes[i-1] >= 'A' && runes[i-1] <= 'Z'):
			spaced.WriteRune(' ')
			spaced.WriteRune(r)
		default:
			spaced.WriteRune(r)
		}
	}
	text := strings.TrimSpace(spaced.String())
	if text == "" {
		return "field"
	}
	// Sentence case: capitalise the first rune, lower the rest.
	lower := strings.ToLower(text)
	first := strings.ToUpper(string([]rune(lower)[:1]))
	return first + lower[1:]
}

// errorAs is a tiny shim so this file does not need the errors package for one
// call site, keeping the import list focused.
func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **validator.InvalidValidationError:
		invalid, ok := err.(*validator.InvalidValidationError)
		if !ok {
			return false
		}
		*t = invalid
		return true
	default:
		return false
	}
}

// ValidationRule documents the tag syntax accepted in request structs. It exists
// so that the syntax has one canonical description in the codebase.
//
//	validate:"required"                     field must be present and non-zero
//	validate:"required,min=3,max=120"       length or numeric bounds
//	validate:"email"                        valid email address
//	validate:"oneof=draft published"        value must be one of the options
//	validate:"eqfield=PasswordConfirmation" must equal another field
//	validate:"unique=users,email"           custom rule registered by the app
//
// TODO(phase-2): ship database-aware rules (unique, exists) that take a model
// name, resolved through the query wrapper.
type ValidationRule string

// String implements fmt.Stringer for nice debugging output.
func (r ValidationRule) String() string { return string(r) }

// Atoi is a thin exported wrapper used by generated request stubs that need to
// coerce a numeric parameter from a rule.
func Atoi(s string) (int, error) { return strconv.Atoi(s) }
