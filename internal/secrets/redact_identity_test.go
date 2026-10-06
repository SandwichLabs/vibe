package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactIdentityHidesTheUserNameButNotPatternsRedactorTargets(t *testing.T) {
	in := `IO Error: Unable to connect to Postgres at user='rosa' password='***' host='db' port='5432': FATAL:  password authentication failed for user "rosa"`
	out := RedactIdentity(in)
	assert.NotContains(t, out, "rosa")
	assert.Contains(t, out, "host='db'", "only the identity goes")
	assert.Contains(t, out, `user='***'`)
	assert.Contains(t, RedactIdentity("conn user=bob dbname=x"), "user='***' dbname=x")
	assert.Equal(t, "nothing to hide here", RedactIdentity("nothing to hide here"))
}
