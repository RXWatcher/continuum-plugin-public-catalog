package store

import (
	"crypto/subtle"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// CatalogPasswordVerifyResult signals whether a supplied catalog
// password matched the stored hash, and if so whether the stored hash
// should be upgraded (legacy plaintext → bcrypt).
type CatalogPasswordVerifyResult int

const (
	// CatalogPasswordFail means the supplied password did not match.
	CatalogPasswordFail CatalogPasswordVerifyResult = iota
	// CatalogPasswordOK means the password matched a current-format
	// (bcrypt) hash.
	CatalogPasswordOK
	// CatalogPasswordOKRehash means the password matched a legacy
	// plaintext value stored as catalog_password. The caller should
	// invoke Store.RehashCatalogPassword to upgrade storage.
	CatalogPasswordOKRehash
)

// HashCatalogPassword returns a bcrypt hash for storage. Empty input
// produces an empty hash so callers can use the same code path for
// "no password set".
func HashCatalogPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash catalog password: %w", err)
	}
	return string(hash), nil
}

// VerifyCatalogPassword compares a supplied password against a stored
// bcrypt hash, falling back to a constant-time compare against a legacy
// plaintext value. Returns CatalogPasswordOKRehash when the legacy path
// matched so the caller can upgrade.
func VerifyCatalogPassword(storedHash, legacyPlaintext, supplied string) CatalogPasswordVerifyResult {
	if storedHash != "" && strings.HasPrefix(storedHash, "$2") {
		if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(supplied)); err == nil {
			return CatalogPasswordOK
		}
		// fall through to legacy compare so a stored hash plus a stale
		// plaintext can't shadow each other; legacy compare is also
		// constant-time
	}
	if legacyPlaintext != "" &&
		subtle.ConstantTimeCompare([]byte(legacyPlaintext), []byte(supplied)) == 1 {
		return CatalogPasswordOKRehash
	}
	return CatalogPasswordFail
}
