package vuepher

import (
	"time"

	"github.com/vuepher/vuepher/orm"
)

// Model is the base model every application model embeds (DB-02). It is
// re-exported from the orm package so that application code can write
// vuepher.Model and stay decoupled from where the ORM implementation lives.
//
//	type Post struct {
//		vuepher.Model
//		Title string `json:"title"`
//		Body  string `json:"body"`
//	}
//
// Model provides a primary key, created/updated timestamps and soft deletion.
type Model = orm.Model

// SoftDelete is the deleted-at column type used for soft deletion. Rows are
// marked rather than removed, and the query wrapper excludes them by default
// (DB-02).
type SoftDelete = orm.SoftDelete

// Time is the timestamp type used for created_at and updated_at, aliased so that
// generated models do not need to import time directly.
type Time = time.Time
