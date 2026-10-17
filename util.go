package vuepher

import (
	"crypto/sha256"
	"sort"
)

// sortStrings sorts in place. Wrapping sort.Strings keeps the call sites in this
// package free of an extra import and gives us one place to switch to
// slices.Sort when the minimum Go version allows dropping the dependency-free
// shim.
func sortStrings(items []string) {
	sort.Strings(items)
}

// sha256Sum returns the SHA-256 digest of data. Used to stretch an application
// key of arbitrary length into an AES-256 key (see deriveKey).
func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
