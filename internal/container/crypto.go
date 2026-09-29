package container

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	sqlite3 "github.com/mutecomm/go-sqlcipher/v4"
)

// ErrWrongPassphrase is the sentinel verifyPassphrase wraps its underlying
// SQLCipher error in. SQLCipher can't distinguish a wrong key from "not a
// SQLCipher database at all" — both fail the same sanity query the same
// way, with the driver reporting SQLITE_NOTADB (code 26) for both — so the
// sentinel names both possibilities; callers (Studio's OpenFile) turn it
// into a single user-facing message rather than trying to tell the two
// apart. Other verify failures (permission denied, a directory, a locked
// file, I/O errors) are distinct problems and keep their own error.
var ErrWrongPassphrase = errors.New("wrong passphrase, or not a .vibe file")

// buildDSN creates a SQLCipher DSN with the passphrase and encryption settings.
// go-sqlcipher v4 requires the key to be set via the connection string.
func buildDSN(path, passphrase string) string {
	params := url.Values{}
	params.Set("_pragma_key", passphrase)
	params.Set("_pragma_cipher_page_size", "4096")
	params.Set("_pragma_kdf_iter", "256000")
	params.Set("_pragma_cipher_hmac_algorithm", "HMAC_SHA512")
	params.Set("_pragma_cipher_kdf_algorithm", "PBKDF2_HMAC_SHA512")
	return fmt.Sprintf("%s?%s", path, params.Encode())
}

// openSQLCipher opens a SQLCipher database with the given passphrase.
func openSQLCipher(path, passphrase string) (*sql.DB, error) {
	dsn := buildDSN(path, passphrase)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLCipher database: %w", err)
	}
	// SQLCipher requires a single connection to maintain PRAGMA state.
	db.SetMaxOpenConns(1)
	return db, nil
}

// verifyPassphrase tests that the passphrase is correct by attempting a simple query.
// SQLCipher will return an error if the key is wrong.
func verifyPassphrase(db *sql.DB) error {
	_, err := db.Exec("SELECT count(*) FROM sqlite_master;")
	if err != nil {
		var se sqlite3.Error
		if errors.As(err, &se) && se.Code == sqlite3.ErrNotADB {
			return fmt.Errorf("%w: %w", ErrWrongPassphrase, err)
		}
		return fmt.Errorf("failed to verify passphrase (wrong key or corrupt database): %w", err)
	}
	return nil
}
