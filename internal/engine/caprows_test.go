package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapRowsPinsExactSQL(t *testing.T) {
	assert.Equal(t, "SELECT * FROM (SELECT 1) AS _vibe_repl_cap LIMIT 11", CapRows("SELECT 1", 11))
	assert.Equal(t, "SELECT * FROM (WITH x AS (SELECT 2) SELECT * FROM x) AS _vibe_repl_cap LIMIT 1",
		CapRows("WITH x AS (SELECT 2) SELECT * FROM x", 1))
}

func TestCapRowsIfSafe(t *testing.T) {
	cases := []struct {
		name, in string
		ro       bool
		want     string
	}{
		{"select", "SELECT 1", true, "SELECT * FROM (SELECT 1) AS _vibe_repl_cap LIMIT 5"},
		{"trailing semicolon trimmed", "  select 1 ;  ", true, "SELECT * FROM (select 1 ) AS _vibe_repl_cap LIMIT 5"},
		{"with", "WITH x AS (SELECT 1) SELECT * FROM x", true, "SELECT * FROM (WITH x AS (SELECT 1) SELECT * FROM x) AS _vibe_repl_cap LIMIT 5"},
		{"not read only", "SELECT 1", false, "SELECT 1"},
		{"own limit", "SELECT 1 LIMIT 2", true, "SELECT 1 LIMIT 2"},
		{"multi statement", "SELECT 1; SELECT 2", true, "SELECT 1; SELECT 2"},
		{"show", "SHOW TABLES", true, "SHOW TABLES"},
		{"empty", "  ", true, "  "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, CapRowsIfSafe(c.in, c.ro, 5))
		})
	}
}
