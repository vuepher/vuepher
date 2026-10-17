package migrations

import (
	"github.com/vuepher/vuepher/orm"

	"blog/app/models"
)

// CreateUsersTable creates the users table.
//
// The migration derives its columns from the model rather than repeating them,
// so adding a field to models.User and re-running `vuepher migrate` adds the
// column. That is a deliberate trade: it is convenient during development and
// still honest in production, where a change to an existing column should be a
// new migration rather than an edit to this one.
type CreateUsersTable struct {
}

// Up creates the table.
func (CreateUsersTable) Up(db *orm.DB) error {
	return db.Schema().Create(&models.User{})
}

// Down drops the table. Dropping users also drops every session that references
// them, which is why this is not run by mistake: migrate:rollback asks first.
func (CreateUsersTable) Down(db *orm.DB) error {
	return db.Schema().Drop(&models.User{})
}
