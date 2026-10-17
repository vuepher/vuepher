// Package auth provides session authentication: a guard bound to the session,
// password hashing over bcrypt and argon2id, and the `auth` and `guest`
// middleware (HTTP-09).
//
// The guard is deliberately small. It answers three questions — is anyone
// signed in, who, and what is their identifier — and delegates finding a user to
// a Provider the application supplies.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/vuepher/vuepher"
)

// BCryptHasher hashes passwords with bcrypt.
//
// bcrypt is the default because it is well understood, has a fixed output size
// and is cheap to verify. It truncates input at 72 bytes, which is handled by
// rejecting longer passwords rather than silently ignoring the tail.
type BCryptHasher struct {
	// Cost is the bcrypt cost. Zero means bcrypt.DefaultCost (10).
	Cost int
}

// NewBCryptHasher builds a bcrypt hasher.
func NewBCryptHasher(cost int) *BCryptHasher { return &BCryptHasher{Cost: cost} }

// Hash implements vuepher.Hasher.
func (h *BCryptHasher) Hash(plain string) (string, error) {
	if len(plain) > 72 {
		return "", fmt.Errorf(
			"auth: bcrypt cannot hash passwords longer than 72 bytes; the password given is %d bytes. "+
				"Switch to argon2id, which has no length limit",
			len(plain),
		)
	}
	cost := h.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	encoded, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// Verify implements vuepher.Hasher.
func (h *BCryptHasher) Verify(plain, encoded string) bool {
	return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(plain)) == nil
}

// NeedsRehash implements vuepher.Hasher.
func (h *BCryptHasher) NeedsRehash(encoded string) bool {
	cost, err := bcrypt.Cost([]byte(encoded))
	if err != nil {
		return true
	}
	target := h.Cost
	if target == 0 {
		target = bcrypt.DefaultCost
	}
	return cost < target
}

// Argon2Params are the argon2id cost parameters. The defaults follow the
// RFC 9106 recommendation for a 64 MiB, single-threaded profile and are a
// sensible starting point on a small VPS.
type Argon2Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params returns the recommended parameters.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// Argon2Hasher hashes passwords with argon2id, encoded in the standard PHC
// string format:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
//
// argon2id is preferred for new applications because it is memory-hard and has
// no input length limit, which makes it a better fit for password managers that
// generate very long passwords.
type Argon2Hasher struct {
	Params Argon2Params
}

// NewArgon2Hasher builds an argon2id hasher with the recommended parameters.
func NewArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{Params: DefaultArgon2Params()}
}

// ErrInvalidHash reports a stored hash that this package cannot parse. It
// usually means the column was truncated.
var ErrInvalidHash = errors.New("auth: stored password hash is not in a recognised format")

// Hash implements vuepher.Hasher.
func (h *Argon2Hasher) Hash(plain string) (string, error) {
	params := h.Params
	if params.Memory == 0 {
		params = DefaultArgon2Params()
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: could not generate a salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.Memory, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify implements vuepher.Hasher.
func (h *Argon2Hasher) Verify(plain, encoded string) bool {
	params, salt, expected, err := decodeArgon2(encoded)
	if err != nil {
		return false
	}
	key := argon2.IDKey([]byte(plain), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(key, expected) == 1
}

// NeedsRehash implements vuepher.Hasher.
func (h *Argon2Hasher) NeedsRehash(encoded string) bool {
	params, _, _, err := decodeArgon2(encoded)
	if err != nil {
		return true
	}
	current := h.Params
	if current.Memory == 0 {
		current = DefaultArgon2Params()
	}
	return params.Memory < current.Memory ||
		params.Iterations < current.Iterations ||
		params.Parallelism < current.Parallelism
}

// decodeArgon2 parses a PHC-format argon2id hash.
func decodeArgon2(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	if parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}

	var params Argon2Params
	for _, field := range strings.Split(parts[3], ",") {
		key, value, found := strings.Cut(field, "=")
		if !found {
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
		number, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
		switch key {
		case "m":
			params.Memory = uint32(number)
		case "t":
			params.Iterations = uint32(number)
		case "p":
			if number > 255 {
				return Argon2Params{}, nil, nil, ErrInvalidHash
			}
			params.Parallelism = uint8(number)
		default:
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	return params, salt, key, nil
}

// DefaultHasher returns the hasher new applications should use. It is bcrypt,
// because the wider ecosystem of tooling and documentation around managing
// bcrypt hashes makes the migration path easier; argon2id is available for
// applications that need it.
func DefaultHasher() vuepher.Hasher { return NewBCryptHasher(0) }
