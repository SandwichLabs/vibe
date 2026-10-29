package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactPatternsMoreShapes(t *testing.T) {
	for _, in := range []string{
		"Bearer abc12345",
		"bearer tok.en/1",
		"X-Api-Key: abcdef",
		"x-amz-security-token=FwoGZXIvYXdz",
		"X-Goog-Api-Key: AIzaSy",
		"Proxy-Authorization: Basic dXNlcg==",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sig_nature-1 end",
		"key sk-abcdefghijklmnopqrstu end",
		"ghp_abcdefghijklmnopqrstuvwx",
		"gho_abcdefghijklmnopqrstuvwx",
		"glpat-abcdefghijklmnopqrstu",
		"xoxb-123456789012-abcdef",
		"id AKIAIOSFODNN7EXAMPLE end",
		"Bearer abcdefghijklmnopqrstuvwx",    // 16+ letters only
		"basic abcdefghijklmnopqrstuvwxyzab", // likewise
	} {
		out := RedactPatterns(in)
		assert.Contains(t, out, Mask, in)
		assert.NotContains(t, out, "abcdef"+"ghijkl", in)
		assert.NotContains(t, out, "AKIAIOSFODNN7EXAMPLE", in)
		assert.NotContains(t, out, "eyJhbGci", in)
	}
}

func TestRedactPatternsKeepsProse(t *testing.T) {
	for _, in := range []string{
		"SELECT token FROM t",
		"bearer of bad news",
		"bearer certificate",
		"basic usage",
		"X-Request-Id: 12",
		"sk-short",
		"bearer abcdefghijk", // 11 letters, no digit: prose
	} {
		assert.Equal(t, in, RedactPatterns(in), in)
	}
}
