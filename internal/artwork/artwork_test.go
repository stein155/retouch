package artwork

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withServer(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := base
	base = srv.URL
	t.Cleanup(func() { base = old })
	return srv.Client()
}

func TestSearch(t *testing.T) {
	var gotQuery string
	c := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"results":[{"artworkUrl100":"https://cdn/x/100x100bb.jpg"}]}`))
	})
	got, err := Search(context.Background(), c, "Charles & Eddie Would I Lie To You")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "https://cdn/x/600x600bb.jpg" {
		t.Errorf("Search = %q, want upgraded to 600x600", got)
	}
	if gotQuery == "" || !contains(gotQuery, "term=") {
		t.Errorf("query = %q, want a term parameter", gotQuery)
	}
}

func TestSearchNoResults(t *testing.T) {
	c := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	got, err := Search(context.Background(), c, "nothing matches")
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestSearchEmptyTerm(t *testing.T) {
	// No term -> no request, empty result, no error.
	got, err := Search(context.Background(), http.DefaultClient, "   ")
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestSearchNon200(t *testing.T) {
	c := withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	if _, err := Search(context.Background(), c, "x"); err == nil {
		t.Error("expected error on non-200")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
