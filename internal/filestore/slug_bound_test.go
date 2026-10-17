package filestore

import "testing"

func TestUniqueSlug_GivesUpWhenEverythingExists(t *testing.T) {
	if got := UniqueSlug("a", func(string) bool { return true }); got != "" {
		t.Fatalf("got %q, want an empty result", got)
	}
	if got := UniqueSlug("a", func(c string) bool { return c != "a-3" }); got != "a-3" {
		t.Fatalf("got %q", got)
	}
}
