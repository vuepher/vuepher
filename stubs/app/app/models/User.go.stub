package models

import (
	"fmt"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/auth"
	"github.com/vuepher/vuepher/orm"
)

// User is the application's user model.
//
// It satisfies auth.Authenticatable through AuthID and AuthPassword, which is
// all the session guard needs. The password column is tagged `json:"-"` so a
// digest can never reach the frontend through the shared `auth` prop.
type User struct {
	vuepher.Model

	Name     string `json:"name" validate:"required,max=120"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"-"`
}

// TableName pins the table name.
func (User) TableName() string { return "users" }

// AuthID implements auth.Authenticatable. The stored identifier is a string so
// that changing the primary key type later does not invalidate live sessions.
func (u User) AuthID() string { return fmt.Sprint(u.ID) }

// AuthPassword implements auth.Authenticatable.
func (u User) AuthPassword() string { return u.Password }

// Query starts a fluent query against users.
func (m User) Query() *orm.Builder[User] {
	return orm.Query[User]()
}

// SetPassword hashes and stores a plaintext password. It never stores the
// plaintext, and it is a method rather than a field assignment so there is one
// place that can get this wrong.
func (u *User) SetPassword(plain string, hasher vuepher.Hasher) error {
	if hasher == nil {
		hasher = auth.DefaultHasher()
	}
	digest, err := hasher.Hash(plain)
	if err != nil {
		return err
	}
	u.Password = digest
	return nil
}

// EmailTaken reports whether an email is already registered, so a registration
// handler can return a field error instead of a database constraint failure.
func EmailTaken(db *orm.DB, email string) (bool, error) {
	return orm.QueryOn[User](db).Where("email = ?", email).Exists()
}
