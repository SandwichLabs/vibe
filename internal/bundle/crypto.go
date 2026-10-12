package bundle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// ageMagic starts every binary age file. It is the unencrypted header that
// tells an importer to ask for a passphrase.
var ageMagic = []byte("age-encryption.org/")

var (
	zipMagic    = []byte("PK\x03\x04")
	sqliteMagic = []byte("SQLite format 3\x00")
)

// maxScryptWork is the highest scrypt work factor Open accepts (log2 N): 18,
// age's default and what Vibe itself encrypts with (about a second, 256 MiB).
// A hostile file claiming more would make every passphrase guess cost seconds
// and gigabytes.
const maxScryptWork = 18

// Info says what a file is before any passphrase is tried.
type Info struct {
	Encrypted bool `json:"encrypted"`
}

// armorMagic starts an ASCII-armored age file.
var armorMagic = []byte("-----BEGIN AGE ENCRYPTED FILE-----")

func isArmored(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), armorMagic)
}

// Peek classifies data without decrypting it: an age envelope (binary or
// armored), a zip, or an error. Only a file with SQLite's header is called an
// older container (ErrLegacy); an SQLCipher file has no header at all, so
// anything else that is not a zip or an age envelope is just not a bundle.
func Peek(data []byte) (Info, error) {
	switch {
	case bytes.HasPrefix(data, ageMagic), isArmored(data):
		return Info{Encrypted: true}, nil
	case bytes.HasPrefix(data, zipMagic):
		return Info{}, nil
	case bytes.HasPrefix(data, sqliteMagic):
		return Info{}, ErrLegacy
	case len(data) == 0:
		return Info{}, fmt.Errorf("%w: the file is empty", ErrNotBundle)
	}
	return Info{}, ErrNotBundle
}

// encrypt wraps plain in an age envelope with a scrypt passphrase recipient.
func encrypt(plain []byte, passphrase string) ([]byte, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to set up encryption: %w", err)
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, r)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt the bundle: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return nil, fmt.Errorf("failed to encrypt the bundle: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to encrypt the bundle: %w", err)
	}
	return out.Bytes(), nil
}

// declaredWorkFactor reads the scrypt work factor (log2 N) the envelope's
// header asks for, or 0 when there is none. age reports an over-limit factor
// only as error text, so the header is read directly rather than the message
// matched.
func declaredWorkFactor(data []byte) int {
	var src io.Reader = bytes.NewReader(data)
	if isArmored(data) {
		src = armor.NewReader(src)
	}
	head, _ := io.ReadAll(io.LimitReader(src, 4096))
	for _, line := range strings.Split(string(head), "\n") {
		f := strings.Fields(line)
		if len(f) == 4 && f[0] == "->" && f[1] == "scrypt" {
			n, err := strconv.Atoi(f[3])
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

// decrypt opens an age envelope. The plaintext is capped: an authenticated
// stream can still be arbitrarily long.
func decrypt(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, ErrPassphraseRequired
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to set up decryption: %w", err)
	}
	id.SetMaxWorkFactor(maxScryptWork)
	var src io.Reader = bytes.NewReader(data)
	if isArmored(data) {
		src = armor.NewReader(src)
	}
	r, err := age.Decrypt(src, id)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, ErrWrongPassphrase
		}
		if declaredWorkFactor(data) > maxScryptWork {
			return nil, ErrUnsupportedKDF
		}
		return nil, corrupt("the encrypted bundle could not be read (%v)", err)
	}
	plain, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, corrupt("the encrypted bundle is damaged (%v)", err)
	}
	if len(plain) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	return plain, nil
}
