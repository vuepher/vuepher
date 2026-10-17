package migrations

import (
	"github.com/vuepher/vuepher/orm"

	"blog/app/models"
)

// CreatePostsTable creates the posts table.
type CreatePostsTable struct {
}

// Up creates the table and an index for the query the index page runs.
func (CreatePostsTable) Up(db *orm.DB) error {
	if err := db.Schema().Create(&models.Post{}); err != nil {
		return err
	}
	// The index page orders by created_at, so index it. Without this the query
	// degrades to a sort of the whole table as the data grows.
	return db.Schema().Exec(
		"CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts (created_at DESC)",
	)
}

// Down drops the table.
func (CreatePostsTable) Down(db *orm.DB) error {
	return db.Schema().Drop(&models.Post{})
}
