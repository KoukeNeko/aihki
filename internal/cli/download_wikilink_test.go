package cli

import (
	"context"
	"crypto/sha1" // #nosec G505 -- test fixture for Taiga's compatibility digest.
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAttachmentDownloadWritesVerifiedFile(t *testing.T) {
	content := []byte("downloaded attachment")
	digest := sha1.Sum(content) // #nosec G401 -- test fixture.
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/issues/attachments/13":
			_, _ = io.WriteString(w, `{"id":13,"project":1,"object_id":2,"name":"note.txt","size":`+strconv.Itoa(len(content))+`,"url":"`+serverURL+`/media/note.txt?token=signed#refresh=issue:13","sha1":"`+hex.EncodeToString(digest[:])+`"}`)
		case "/media/note.txt":
			_, _ = w.Write(content)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	serverURL = server.URL
	app, out, stderr, _ := testApp(t, server)
	path := filepath.Join(t.TempDir(), "note.txt")
	code := app.Execute(context.Background(), []string{"--json", "attachment", "download", "issue", "13", "--output", path})
	if code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(content) {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if !strings.Contains(out.String(), `"verified":true`) || !strings.Contains(out.String(), `"sha256"`) {
		t.Fatalf("output=%s", out.String())
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
}

func TestWikiLinkCommandLifecycle(t *testing.T) {
	link := map[string]any{"id": float64(3), "project": float64(1), "title": "Guide", "href": "guide", "order": float64(10)}
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/wiki-links":
			_ = json.NewEncoder(w).Encode([]any{link})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/wiki-links":
			_ = json.NewEncoder(w).Encode(link)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/wiki-links/3":
			link["title"] = "Updated"
			_ = json.NewEncoder(w).Encode(link)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/wiki-links/3":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/wiki-links/3" && deleted:
			http.Error(w, `{"detail":"Not found"}`, http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	commands := [][]string{
		{"--json", "wiki-link", "list"},
		{"--json", "wiki-link", "view", "guide"},
		{"--json", "wiki-link", "create", "--title", "Guide"},
		{"--json", "wiki-link", "edit", "guide", "--title", "Updated"},
		{"--json", "wiki-link", "delete", "guide", "--yes"},
	}
	for _, args := range commands {
		app, out, stderr, _ := testApp(t, server)
		if code := app.Execute(context.Background(), args); code != ExitSuccess {
			t.Fatalf("taiga %v exit=%d stderr=%s", args, code, stderr.String())
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("taiga %v output=%s", args, out.String())
		}
	}
}
