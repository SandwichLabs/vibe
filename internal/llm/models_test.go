package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListModels(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"b"},{"id":"a"},{"id":"bad name"}]}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:0.6b","capabilities":["completion","tools"]},{"name":"llama3"},{"name":"embeddinggemma","capabilities":["embedding"]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	l := ListModels(ctx, "local", srv.URL+"/v1")
	assert.Equal(t, []string{"a", "b"}, l.Models)
	assert.Equal(t, "/v1/models", gotPath)
	assert.Empty(t, gotAuth, "no key is sent")

	l = ListModels(ctx, "ollama", srv.URL+"/v1/")
	assert.Equal(t, []string{"llama3", "qwen3:0.6b"}, l.Models)
	assert.Equal(t, "/api/tags", gotPath)

	l = ListModels(ctx, "openai", srv.URL)
	assert.NotEmpty(t, l.Error)
	l = ListModels(ctx, "local", "file:///etc/passwd")
	assert.NotEmpty(t, l.Error)
	l = ListModels(ctx, "local", srv.URL+"/nope")
	assert.Contains(t, l.Error, "404")
}

func TestListModels_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	l := ListModels(context.Background(), "ollama", url+"/v1")
	assert.True(t, l.Unreachable)
	assert.Empty(t, l.Models)
}
