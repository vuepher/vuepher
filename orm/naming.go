package orm

import (
	"reflect"

	"gorm.io/gorm/schema"
)

// defaultNamingStrategy is the single source of truth for table and column
// naming (DB-02, generator naming rules):
//
//	// struct name        -> table name
//	Post                   posts
//	BlogPost               blog_posts
//	CategoryTranslation    category_translations
//
// Column names follow the same rule, so Title becomes title and CreatedAt
// becomes created_at.
//
// GORM's default NamingStrategy already implements this, but it is named here so
// that the CLI generator and the ORM provably agree: TableNameOf is exported and
// the generator calls it rather than reimplementing pluralisation.
func defaultNamingStrategy() schema.Namer {
	return schema.NamingStrategy{
		SingularTable: false,
	}
}

// TableNameOf returns the table name the ORM will use for a model value or type,
// resolving pointers:
//
//	orm.TableNameOf(models.Post{})   // "posts"
//	orm.TableNameOf(&models.Post{})  // "posts"
//	orm.TableNameOf(models.BlogPost{}) // "blog_posts"
//
// The generated migration and the generated model both derive their names from
// this function, so `vuepher make:model BlogPost -m` always produces a migration
// that creates the table the model expects.
func TableNameOf(model any) string {
	if model == nil {
		return ""
	}
	naming := defaultNamingStrategy()

	// A type that declares its own name wins.
	if namer, ok := model.(TableNamer); ok {
		if name := namer.TableName(); name != "" {
			return name
		}
	}
	if name, ok := tableNameFromMethod(model); ok {
		return name
	}

	typ := reflect.TypeOf(model)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Name() == "" {
		return ""
	}
	return naming.TableName(typ.Name())
}

// ColumnNameOf returns the column name for a Go field name:
//
//	orm.ColumnNameOf("CreatedAt")  // "created_at"
func ColumnNameOf(field string) string {
	return defaultNamingStrategy().ColumnName("", field)
}

// tableNameFromMethod looks for a TableName() string method produced by a
// generated model, without requiring the TableNamer interface to be asserted on
// a pointer receiver.
func tableNameFromMethod(model any) (string, bool) {
	value := reflect.ValueOf(model)
	method := value.MethodByName("TableName")
	if !method.IsValid() {
		return "", false
	}
	results := method.Call(nil)
	if len(results) != 1 || results[0].Kind() != reflect.String {
		return "", false
	}
	name := results[0].String()
	return name, name != ""
}
