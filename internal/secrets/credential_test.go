package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCredentialOverlay_TypedWinsAndTheBindingStays(t *testing.T) {
	base := Credential{User: "rosa", Password: "stored", Token: "tok", Fingerprint: "fp", Server: "db:5432"}
	got := base.Overlay(Credential{User: "bob", Secret: "s3"})
	assert.Equal(t, "bob", got.User)
	assert.Equal(t, "stored", got.Password, "an untyped field keeps the stored value")
	assert.Equal(t, "s3", got.Secret)
	assert.Equal(t, "fp", got.Fingerprint)
	assert.Equal(t, "db:5432", got.Server)
	assert.Equal(t, "rosa", base.User, "Overlay does not change the receiver")
	assert.Equal(t, base, base.Overlay(Credential{}))
}
