package connections

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/SandwichLabs/vibe/internal/session"
)

// Everything person-facing (the extensions list, the consent dialog, the CLI's
// --install) reads TypeInfo.Extensions, while the session gates on the type's
// Extensions(). They must be the same list, in the same order.
func TestInfoExtensionsMatchTheTypeForEveryType(t *testing.T) {
	for name, typ := range registry {
		ct, ok := typ.(session.CredentialedType)
		if !ok {
			continue
		}
		ti, err := TypeInfoFor(name)
		if !assert.NoError(t, err, name) {
			continue
		}
		assert.Equal(t, ct.Extensions(), ti.Extensions, name)
	}
}
