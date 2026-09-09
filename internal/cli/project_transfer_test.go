package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoukeNeko/taiga-cli/internal/credential"
)

func TestProjectExportReportsAcceptedAsyncWork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":7,"name":"Demo","slug":"demo"}`)
		case "/api/v1/exporter/7":
			if r.URL.Query().Get("dump_format") != "gzip" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"export_id":"export-123"}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"--json", "project", "export", "demo"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		Data projectExportView `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Status != "accepted" || envelope.Data.ExportID != "export-123" || envelope.Data.Verified {
		t.Fatalf("data = %#v", envelope.Data)
	}
}

func TestProjectExportDryRunDoesNotEnqueueWork(t *testing.T) {
	exports := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects/by_slug" {
			_, _ = io.WriteString(w, `{"id":7,"name":"Demo","slug":"demo"}`)
			return
		}
		exports++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"--json", "project", "export", "demo", "--format", "plain", "--dry-run"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if exports != 0 || !strings.Contains(out.String(), `"performed":false`) || !strings.Contains(out.String(), `"format":"plain"`) {
		t.Fatalf("exports=%d stdout=%s", exports, out.String())
	}
}

func TestProjectImportDryRunValidatesGzipWithoutNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json.gz")
	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	_, _ = io.WriteString(writer, `{"name":"Imported Demo","slug":"imported-demo","is_private":true,"roles":[{"name":"Owner"}]}`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, stderr, _ := testApp(t, nil)
	if code := app.Execute(context.Background(), []string{"--json", "project", "import", path, "--dry-run"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		Plan struct {
			Performed bool           `json:"performed"`
			Changes   map[string]any `json:"changes"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Plan.Performed || envelope.Plan.Changes["format"] != "gzip" || envelope.Plan.Changes["name"] != "Imported Demo" || envelope.Plan.Changes["private"] != true {
		t.Fatalf("plan = %#v", envelope.Plan)
	}
}

func TestProjectImportRejectsInvalidDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`[{"name":"not an object"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, stderr, _ := testApp(t, nil)
	code := app.Execute(context.Background(), []string{"--json", "project", "import", path, "--dry-run"})
	if code != ExitValidation || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid_project_dump") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestProjectImportRequiresConfirmationBeforeUpload(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"name":"Imported Demo","roles":[{"name":"Owner"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "--no-input", "project", "import", path})
	if code != ExitConfirmationRequired || out.Len() != 0 || posts != 0 || !strings.Contains(stderr.String(), "confirmation_required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s posts=%d", code, out.String(), stderr.String(), posts)
	}
}

func TestProjectImportFromStdinRequiresConfirmationBeforeReading(t *testing.T) {
	dump := `{"name":"Imported Demo","roles":[{"name":"Owner"}]}`
	input := strings.NewReader(dump)
	app, out, stderr, _ := testApp(t, nil)
	app.In = input
	code := app.Execute(context.Background(), []string{"--json", "project", "import", "-"})
	if code != ExitConfirmationRequired || out.Len() != 0 || input.Len() != len(dump) || !strings.Contains(stderr.String(), "confirmation_required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s unread=%d", code, out.String(), stderr.String(), input.Len())
	}
}

func TestProjectImportReportsSynchronouslyCreatedProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The credential is exercised before the dump is streamed.
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/me" {
			_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/importer/load_dump" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		file, _, err := r.FormFile("dump")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		data, _ := io.ReadAll(file)
		if !bytes.Contains(data, []byte(`"name":"Imported Demo"`)) {
			t.Fatalf("dump = %s", data)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":8,"name":"Imported Demo","slug":"imported-demo","is_private":true}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"name":"Imported Demo","is_private":true,"roles":[{"name":"Owner"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"--json", "project", "import", path, "--yes"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		Data projectImportView `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Status != "created" || !envelope.Data.Verified || envelope.Data.Project == nil || envelope.Data.Project.Slug != "imported-demo" {
		t.Fatalf("data = %#v", envelope.Data)
	}
}

// The dump is streamed, and a stream cannot be resent after a refresh the way
// a JSON request is. Every other command touches Taiga with a JSON lookup
// before it streams, which is where an expired token gets refreshed; import
// has no lookup of its own, so it has to exercise the credential first.
func TestProjectImportRefreshesAnExpiredTokenBeforeStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			_, _ = io.WriteString(w, `{"auth_token":"fresh","refresh":"fresh-refresh"}`)
			return
		case "/api/v1/users/me", "/api/v1/importer/load_dump":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"detail":"Token is expired"}`)
				return
			}
			if r.URL.Path == "/api/v1/users/me" {
				_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"import_id":"import-1"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(path, []byte(`{"name":"Imported","slug":"imported"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, stderr, credentials := testApp(t, server)
	credentials.values[credential.Account("test", server.URL+"/api/v1/")] = credential.Tokens{AuthToken: "stale", RefreshToken: "old-refresh"}

	if code := app.Execute(context.Background(), []string{"--json", "project", "import", path, "--yes"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"import_id":"import-1"`) {
		t.Fatalf("stdout = %s", out.String())
	}
	if saved := credentials.values[credential.Account("test", server.URL+"/api/v1/")]; saved.AuthToken != "fresh" || saved.RefreshToken != "fresh-refresh" {
		t.Fatalf("saved tokens = %#v, want the rotated pair", saved)
	}
}
