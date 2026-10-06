package secrets

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactPatternsTable(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"pg quoted", "at user='rosa' password='pw1x' host='h'", "at user='rosa' password='***' host='h'"},
		{"unquoted", "password=pw1x tail", "password=*** tail"},
		{"aws", "aws_secret_access_key=AbCd1234 ok", "aws_secret_access_key=*** ok"},
		{"s3", "s3_secret_access_key = 'AbCd1234'", "s3_secret_access_key = '***'"},
		{"motherduck", "motherduck_token=eyJhbGciOi.x.y rest", "motherduck_token=*** rest"},
		{"session token", "session_token=Qwerty123", "session_token=***"},
		{"client secret", "client_secret=abc123xyz", "client_secret=***"},
		{"PGPASSWORD", "PGPASSWORD=hunter22 psql", "PGPASSWORD=*** psql"},
		{"ddl password", "CREATE SECRET s (TYPE postgres, PASSWORD 'pw1x', USER 'rosa')", "CREATE SECRET s (TYPE postgres, PASSWORD '***', USER 'rosa')"},
		{"ddl secret", "(KEY_ID 'AKIA1', SECRET 'sh''hh')", "(KEY_ID 'AKIA1', SECRET '***')"},
		{"ddl token", "BEARER_TOKEN 'abc'", "BEARER_TOKEN '***'"},
		{"url userinfo", "connect postgres://rosa:pw1x@db.example:5432/app failed", "connect postgres://rosa:***@db.example:5432/app failed"},
		{"json", `{"user":"rosa","password": "pw\"1x"}`, `{"user":"rosa","password": "***"}`},
		{"yaml", "user: rosa\npassword: pw1x\nhost: h", "user: rosa\npassword: ***\nhost: h"},
		{"bearer", "Authorization: Bearer abc.DEF-123_x end", "Authorization: Bearer *** end"},
		{"primary key kept", "violates primary key=id and key: id", "violates primary key=id and key: id"},
		{"escaped quote", `password='a\'b c' end`, "password='***' end"},
		{"basic prose", "basic usage of the thing. Basic Input: yes", "basic usage of the thing. Basic Input: yes"},
		{"bearer prose", "the bearer of this news", "the bearer of this news"},
		{"long bearer", "got bearer abcdefghijklmnop1234 ok", "got bearer *** ok"},
		{"max_tokens", "max_tokens=4096 and token_count = 12", "max_tokens=4096 and token_count = 12"},
		{"tokenizer", "tokenizer: fast mode\ntokens: 5", "tokenizer: fast mode\ntokens: 5"},
		{"token exact", "token=abc123 access_token=zzz111 bearer_token = 'q'", "token=*** access_token=*** bearer_token = '***'"},
		{"authorization basic", "Authorization: Basic dXNlcjpwdw==", "Authorization: Basic ***"},
		{"plain untouched", "Catalog Error: Table foo does not exist", "Catalog Error: Table foo does not exist"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RedactPatterns(c.in)
			assert.Equal(t, c.want, got)
			assert.Equal(t, got, RedactPatterns(got), "idempotent")
		})
	}
}

func TestRedactorValueVariants(t *testing.T) {
	r := NewRedactor()
	r.Add("it's a/pw&x=1\"q")
	for _, echoed := range []string{
		"it's a/pw&x=1\"q",
		"it''s a/pw&x=1\"q",
		`it\'s a/pw&x=1"q`,
		"it%27s+a%2Fpw%26x%3D1%22q",
		`it's a/pw&x=1\"q`,
	} {
		got := r.Redact("error near " + echoed + " end")
		assert.NotContains(t, got, "pw&x", echoed)
		assert.NotContains(t, got, "pw%26x", echoed)
		assert.Contains(t, got, "end")
	}
}

func TestRedactingHandlerByteSlice(t *testing.T) {
	r := NewRedactor()
	r.Add("hunter2-NEEDLE")
	var buf bytes.Buffer
	slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil), r)).Info("x", "body", []byte("password=hunter2-NEEDLE; hunter2-NEEDLE"))
	assert.NotContains(t, buf.String(), "NEEDLE")
}
