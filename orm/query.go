package orm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Builder is the fluent, type-safe query builder (DB-03). It wraps GORM's chain
// but carries the model type as a parameter, so results come back as []Post
// rather than []any and the compiler catches a mismatched assignment.
//
// It is called Builder rather than Query because Go does not allow a generic
// function and a generic type to share a name, and the constructor — Query[T]() —
// is the name the API is designed around.
//
//	posts, err := orm.Query[Post]().
//		Where("published = ?", true).
//		Latest().
//		With("Comments").
//		Get()
//
// Every method is chainable and none of them talk to the database; the terminal
// methods (Get, First, Find, Count, Exists, Paginate, Chunk) execute the query.
//
// The builder is not safe to share across goroutines.
type Builder[T any] struct {
	db   *DB
	stmt *gorm.DB

	// err records the first failure. A failed chain keeps returning itself so
	// callers do not have to check after every step, and the terminal method
	// returns the original error with the statement that caused it.
	err  error
	step string

	// conditions counts the filtering clauses added so far. Update and Delete
	// require at least one, so that a forgotten Where cannot wipe a table.
	conditions int
}

// Query starts a query for T on the default connection.
//
// Application models expose this as a method, which is what makes the Eloquent
// style work. Go does not allow a generic function and a generic type to share a
// name, so the constructor is Query[T]() and the builder it returns is Builder[T]:
//
//	func (m Post) Query() *orm.Builder[Post] { return orm.Query[Post]() }
//
//	models.Post{}.Query().Latest().Paginate(ctx, 15)
func Query[T any]() *Builder[T] { return QueryOn[T](Default()) }

// QueryOn starts a query for T on a specific connection, for example inside a
// transaction:
//
//	db.Transaction(ctx, func(tx *orm.DB) error {
//		return orm.QueryOn[Post](tx).Create(&post)
//	})
func QueryOn[T any](db *DB) *Builder[T] {
	q := &Builder[T]{db: db}
	if db == nil || db.DB == nil {
		q.err = ErrNoConnection
		q.step = "Query"
		return q
	}
	q.stmt = db.raw().Model(new(T))
	return q
}

// Gorm exposes the underlying statement (DB-10). Use it to reach anything the
// wrapper does not cover, then keep chaining by wrapping the result with
// FromGorm.
func (q *Builder[T]) Gorm() *gorm.DB { return q.stmt }

// FromGorm continues a chain from a raw GORM statement, the bridge for dropping
// down to GORM and coming back.
func (q *Builder[T]) FromGorm(stmt *gorm.DB) *Builder[T] {
	q.stmt = stmt
	return q
}

// Clone returns an independent copy of the chain. Use it when one set of
// conditions has to be reused for a query and a count.
func (q *Builder[T]) Clone() *Builder[T] {
	if q.stmt == nil {
		return &Builder[T]{db: q.db, err: q.err, step: q.step}
	}
	return &Builder[T]{db: q.db, stmt: q.stmt.Session(&gorm.Session{}), err: q.err, step: q.step}
}

// fail records an error and the step that produced it.
func (q *Builder[T]) fail(step string, err error) *Builder[T] {
	if q.err == nil {
		q.err = err
		q.step = step
	}
	return q
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

// Where adds a condition. Args are bound as parameters, never interpolated, so
// passing user input here is safe.
//
//	orm.Query[Post]().Where("published_at <= ?", now)
//	orm.Query[Post]().Where(&Post{AuthorID: 7})
func (q *Builder[T]) Where(condition any, args ...any) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Where(condition, args...)
	q.conditions++
	return q
}

// WhereIn adds an IN condition.
func (q *Builder[T]) WhereIn(column string, values any) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Where(column+" IN ?", values)
	q.conditions++
	return q
}

// WhereNotIn adds a NOT IN condition.
func (q *Builder[T]) WhereNotIn(column string, values any) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Where(column+" NOT IN ?", values)
	q.conditions++
	return q
}

// WhereNull adds an IS NULL condition.
func (q *Builder[T]) WhereNull(column string) *Builder[T] {
	return q.Where(column + " IS NULL")
}

// WhereNotNull adds an IS NOT NULL condition.
func (q *Builder[T]) WhereNotNull(column string) *Builder[T] {
	return q.Where(column + " IS NOT NULL")
}

// OrWhere adds an OR-ed condition, for the group beginning at the previous
// Where.
func (q *Builder[T]) OrWhere(condition any, args ...any) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Or(condition, args...)
	q.conditions++
	return q
}

// WhereLike adds a case-insensitive-ish LIKE condition. The caller supplies the
// wildcards, so an escaped prefix search stays possible.
func (q *Builder[T]) WhereLike(column, pattern string) *Builder[T] {
	return q.Where(column+" LIKE ?", pattern)
}

// When conditionally applies a set of clauses, which keeps optional filters
// readable:
//
//	orm.Query[Post]().
//		When(search != "", func(q *orm.Query[Post]) *orm.Query[Post] {
//			return q.WhereLike("title", "%"+search+"%")
//		}).
//		Get()
func (q *Builder[T]) When(condition bool, fn func(*Builder[T]) *Builder[T]) *Builder[T] {
	if !condition || q.err != nil {
		return q
	}
	return fn(q)
}

// Scope applies named query scopes (DB-07). A scope is just a function, so it
// can be shared between models or stored on the model as a method:
//
//	func Published(q *orm.Query[Post]) *orm.Query[Post] {
//		return q.WhereNotNull("published_at")
//	}
func (q *Builder[T]) Scope(scopes ...func(*Builder[T]) *Builder[T]) *Builder[T] {
	for _, scope := range scopes {
		if q.err != nil {
			return q
		}
		q = scope(q)
	}
	return q
}

// Scopes applies raw GORM scopes, the escape hatch for scopes written against
// gorm.DB.
func (q *Builder[T]) Scopes(scopes ...func(*gorm.DB) *gorm.DB) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Scopes(scopes...)
	return q
}

// IncludeSoftDeleted makes the query consider rows marked as deleted (DB-02).
func (q *Builder[T]) IncludeSoftDeleted() *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Unscoped()
	return q
}

// ---------------------------------------------------------------------------
// Ordering, limiting and loading
// ---------------------------------------------------------------------------

// OrderBy adds an ordering clause. The column is validated against a simple
// whitelist pattern because it cannot be bound as a parameter; passing raw user
// input here would be an injection.
func (q *Builder[T]) OrderBy(column string, direction ...string) *Builder[T] {
	if q.err != nil {
		return q
	}
	if !safeIdentifier(column) {
		return q.fail("OrderBy", fmt.Errorf("orm: unsafe column name %q in OrderBy", column))
	}
	dir := "asc"
	if len(direction) > 0 {
		switch strings.ToLower(direction[0]) {
		case "desc":
			dir = "desc"
		case "asc", "":
			dir = "asc"
		default:
			return q.fail("OrderBy", fmt.Errorf("orm: invalid sort direction %q", direction[0]))
		}
	}
	q.stmt = q.stmt.Order(column + " " + strings.ToUpper(dir))
	return q
}

// Latest orders by created_at descending, or by the given columns (DB-03).
func (q *Builder[T]) Latest(columns ...string) *Builder[T] {
	if len(columns) == 0 {
		columns = []string{"created_at"}
	}
	for _, column := range columns {
		q = q.OrderBy(column, "desc")
	}
	return q
}

// Oldest orders by created_at ascending, or by the given columns.
func (q *Builder[T]) Oldest(columns ...string) *Builder[T] {
	if len(columns) == 0 {
		columns = []string{"created_at"}
	}
	for _, column := range columns {
		q = q.OrderBy(column, "asc")
	}
	return q
}

// Limit caps the number of rows returned.
func (q *Builder[T]) Limit(n int) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Limit(n)
	return q
}

// Offset skips rows, used by the paginator.
func (q *Builder[T]) Offset(n int) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Offset(n)
	return q
}

// Select restricts the columns loaded.
func (q *Builder[T]) Select(columns ...string) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Select(columns)
	return q
}

// With eager-loads relations (DB-04). The names are relation fields on the
// model:
//
//	orm.Query[Post]().With("Comments", "Author").Get()
//
// TODO(phase-2): accept a constraint function per relation
// (`With("Comments", func(q *orm.Query[Comment]) *orm.Query[Comment] { ... })`)
// so that eager loads can be filtered and limited.
func (q *Builder[T]) With(relations ...string) *Builder[T] {
	if q.err != nil {
		return q
	}
	for _, relation := range relations {
		q.stmt = q.stmt.Preload(relation)
	}
	return q
}

// Group groups rows by column.
func (q *Builder[T]) Group(columns ...string) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Group(strings.Join(columns, ", "))
	return q
}

// Having adds a condition to a grouped query.
func (q *Builder[T]) Having(condition any, args ...any) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.Having(condition, args...)
	return q
}

// ---------------------------------------------------------------------------
// Terminal operations
// ---------------------------------------------------------------------------

// Get runs the query and returns every matching row.
func (q *Builder[T]) Get() ([]T, error) {
	if q.err != nil {
		return nil, q.err
	}
	var items []T
	if err := q.stmt.Find(&items).Error; err != nil {
		return nil, q.wrap("Get", err)
	}
	return items, nil
}

// First returns the first matching row using the query's ordering, or
// ErrNotFound when nothing matches.
func (q *Builder[T]) First() (*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	var item T
	if err := q.stmt.First(&item).Error; err != nil {
		return nil, q.wrap("First", err)
	}
	return &item, nil
}

// FirstOrFail is First with a clearer name for handler code that treats a miss
// as a 404. It returns ErrNotFound, which handlers translate:
//
//	post, err := models.Post{}.Query().Find(ctx.Param("id"))
//	if errors.Is(err, orm.ErrNotFound) {
//		return vuepher.NotFound("Post not found")
//	}
func (q *Builder[T]) FirstOrFail() (*T, error) { return q.First() }

// Last returns the last matching row, ordered by primary key.
func (q *Builder[T]) Last() (*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	var item T
	if err := q.stmt.Last(&item).Error; err != nil {
		return nil, q.wrap("Last", err)
	}
	return &item, nil
}

// Find loads a single row by primary key.
func (q *Builder[T]) Find(id any) (*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	var item T
	if err := q.stmt.Where("id = ?", id).First(&item).Error; err != nil {
		return nil, q.wrap("Find", err)
	}
	return &item, nil
}

// Count returns the number of matching rows, ignoring any limit and offset.
func (q *Builder[T]) Count() (int64, error) {
	if q.err != nil {
		return 0, q.err
	}
	var total int64
	stmt := q.stmt.Session(&gorm.Session{}).Limit(-1).Offset(-1)
	if err := stmt.Count(&total).Error; err != nil {
		return 0, q.wrap("Count", err)
	}
	return total, nil
}

// Exists reports whether at least one row matches. It reads at most one row, so
// it is cheap on a large table.
func (q *Builder[T]) Exists() (bool, error) {
	if q.err != nil {
		return false, q.err
	}
	var item T
	result := q.stmt.Session(&gorm.Session{}).Limit(1).Find(&item)
	if result.Error != nil {
		return false, q.wrap("Exists", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// Chunk walks the result set in batches, so a large table never has to be held
// in memory at once.
//
//	err := models.Post{}.Query().Chunk(500, func(posts []models.Post) error {
//		for _, post := range posts {
//			reindex(post)
//		}
//		return nil
//	})
func (q *Builder[T]) Chunk(size int, fn func([]T) error) error {
	if q.err != nil {
		return q.err
	}
	if size <= 0 {
		return fmt.Errorf("orm: chunk size must be positive, got %d", size)
	}
	var batch []T
	offset := 0
	for {
		batch = batch[:0]
		stmt := q.stmt.Session(&gorm.Session{}).Limit(size).Offset(offset)
		if err := stmt.Find(&batch).Error; err != nil {
			return q.wrap("Chunk", err)
		}
		if len(batch) == 0 {
			return nil
		}
		if err := fn(batch); err != nil {
			return err
		}
		if len(batch) < size {
			return nil
		}
		offset += size
	}
}

// Create inserts a new row. It fills the model's timestamps and its primary key.
func (q *Builder[T]) Create(model *T) error {
	if q.err != nil {
		return q.err
	}
	if err := q.stmt.Create(model).Error; err != nil {
		return q.wrap("Create", err)
	}
	return nil
}

// Update applies a map of column changes to every row matching the current
// conditions. The guard is deliberate: an update without conditions is almost
// always a mistake, and GORM would happily run it.
func (q *Builder[T]) Update(values map[string]any) error {
	if q.err != nil {
		return q.err
	}
	if len(values) == 0 {
		return nil
	}
	if !q.hasConditions() {
		return fmt.Errorf(
			"orm: refusing to update every row; add a Where clause or use orm.Save for a single model",
		)
	}
	if err := q.stmt.Updates(values).Error; err != nil {
		return q.wrap("Update", err)
	}
	return nil
}

// Save persists a whole model: it inserts when the primary key is zero and
// updates otherwise, including zero-valued fields.
func (q *Builder[T]) Save(model *T) error {
	if q.err != nil {
		return q.err
	}
	if err := q.stmt.Session(&gorm.Session{FullSaveAssociations: false}).Save(model).Error; err != nil {
		return q.wrap("Save", err)
	}
	return nil
}

// Delete removes the matches. A model embedding vuepher.Model is soft-deleted.
// As with Update, deleting without a condition requires ForceDelete.
func (q *Builder[T]) Delete() error {
	if q.err != nil {
		return q.err
	}
	if !q.hasConditions() {
		return fmt.Errorf(
			"orm: refusing to delete every row; add a Where clause or call ForceDelete when that is really intended",
		)
	}
	var zero T
	if err := q.stmt.Delete(&zero).Error; err != nil {
		return q.wrap("Delete", err)
	}
	return nil
}

// ForceDelete removes rows for good: unlike Delete it also removes rows a soft
// delete has already hidden, and it does not require a WHERE clause. Named to be
// impossible to call by accident.
//
// A model embedding vuepher.Model is soft-deleted by Delete, so rows removed that
// way stay in the table. Without Unscoped here, ForceDelete would set deleted_at
// a second time and leave the row in place, which is the opposite of what the
// name promises and the one case — removing personal data — where it matters.
func (q *Builder[T]) ForceDelete() error {
	if q.err != nil {
		return q.err
	}
	var zero T
	if err := q.stmt.Unscoped().Delete(&zero).Error; err != nil {
		return q.wrap("ForceDelete", err)
	}
	return nil
}

// UpdateOrCreate finds the first row matching attributes and updates it, or
// inserts one built from attributes and values.
func (q *Builder[T]) UpdateOrCreate(attributes map[string]any, values map[string]any) (*T, error) {
	if q.err != nil {
		return nil, q.err
	}
	var item T
	stmt := q.stmt.Session(&gorm.Session{}).Where(attributes)
	err := stmt.First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		merged := make(map[string]any, len(attributes)+len(values))
		for k, v := range attributes {
			merged[k] = v
		}
		for k, v := range values {
			merged[k] = v
		}
		if err := q.db.raw().Model(new(T)).Create(merged).Error; err != nil {
			return nil, q.wrap("UpdateOrCreate", err)
		}
		if err := stmt.First(&item).Error; err != nil {
			return nil, q.wrap("UpdateOrCreate", err)
		}
		return &item, nil
	}
	if err != nil {
		return nil, q.wrap("UpdateOrCreate", err)
	}
	if len(values) > 0 {
		if err := q.db.raw().Model(&item).Updates(values).Error; err != nil {
			return nil, q.wrap("UpdateOrCreate", err)
		}
	}
	return &item, nil
}

// Context binds a context so the query is cancelled with the request.
func (q *Builder[T]) Context(ctx context.Context) *Builder[T] {
	if q.err != nil {
		return q
	}
	q.stmt = q.stmt.WithContext(ctx)
	return q
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

// wrap converts GORM's sentinel errors into the wrapper's own vocabulary so that
// handlers only ever compare against orm.ErrNotFound.
func (q *Builder[T]) wrap(step string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("orm: %s failed: %w", step, err)
}

// hasConditions reports whether any filtering clause was added, which is what
// guards the destructive operations.
func (q *Builder[T]) hasConditions() bool { return q.conditions > 0 }

// safeIdentifier reports whether a column name is safe to interpolate: letters,
// digits, underscore, a dot for qualified names, and no SQL metacharacters.
func safeIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
