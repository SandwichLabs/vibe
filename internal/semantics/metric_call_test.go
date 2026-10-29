package semantics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalledMetrics(t *testing.T) {
	assert.Nil(t, CalledMetrics("SELECT 1"))
	assert.Equal(t, []string{"a", "b"}, CalledMetrics("SELECT * FROM metric('b', by => 'x') JOIN METRIC('a') ON true, metric('a')"))
	assert.Nil(t, CalledMetrics("SELECT 'metric(''x'')'"), "inside a string is not a call")
	assert.Nil(t, CalledMetrics("SELECT * FROM metric(foo)"), "not a plain literal: it cannot run")
	assert.Nil(t, CalledMetrics("SELECT x.metric('a')"))
}
