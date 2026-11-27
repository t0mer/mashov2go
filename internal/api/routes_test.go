package api

import (
	"testing"
)

func TestRoutesRegisterOnce(t *testing.T) {
	srv, _ := newServer(t)
	seen := map[string]bool{}
	for _, p := range srv.Patterns() {
		if seen[p] {
			t.Fatalf("route registered twice: %s", p)
		}
		seen[p] = true
	}
	if len(seen) < 40 {
		t.Fatalf("only %d routes recorded", len(seen))
	}
}
