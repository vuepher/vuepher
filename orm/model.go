package orm

import (
	"time"

	"gorm.io/gorm"
)

// SoftDelete is the column type used for soft deletion. It is an alias of
// gorm.DeletedAt, which behaves like a nullable time but makes GORM add
// `deleted_at IS NULL` to every query.
type SoftDelete = gorm.DeletedAt

// Model is embedded by every application model. It provides the primary key and
// timestamps, and enables soft deletion (DB-02):
//
//	type Post struct {
//		orm.Model
//		Title string `json:"title" validate:"required,min=3"`
//	}
//
// Embedding is by value, so the fields take part in the table's columns
// directly. A model that must not be soft-deleted should embed its own key
// instead of using Model, because soft deletion is a property of this struct.
type Model struct {
	// ID is the primary key. uint64 keeps headroom over int32 while staying
	// cheap to index.
	ID uint64 `gorm:"primarykey" json:"id"`

	// CreatedAt is set once on insert.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is refreshed on every save.
	UpdatedAt time.Time `json:"updated_at"`

	// DeletedAt marks a soft-deleted row. It is omitted from JSON so that the
	// frontend does not have to model it; a deleted row should not be visible
	// anyway.
	DeletedAt SoftDelete `gorm:"index" json:"-"`
}

// GetID lets generic code read a model's identifier without reflection at the
// call site.
func (m Model) GetID() uint64 { return m.ID }

// IsDeleted reports whether the model has been soft-deleted.
func (m Model) IsDeleted() bool { return m.DeletedAt.Valid }

// Identifiable is implemented by every model embedding Model. Generic helpers
// accept it so they can work with any model without knowing its concrete type.
type Identifiable interface {
	GetID() uint64
}

// TableNamer lets a model declare its own table name. Vuepher's generator
// produces neither, relying on GORM's pluralising naming strategy, but a model
// migrating from an existing schema can implement this.
type TableNamer interface {
	TableName() string
}
