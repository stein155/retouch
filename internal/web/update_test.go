package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFileRemovesPartialFile(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("short"))
	}))
	t.Cleanup(ts.Close)

	path := filepath.Join(t.TempDir(), "retouch.new")
	s := &Server{version: "test"}
	if err := s.downloadFile(context.Background(), ts.URL, path, 0o755); err == nil {
		t.Fatal("downloadFile succeeded, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial file exists after failed download: %v", err)
	}
}
