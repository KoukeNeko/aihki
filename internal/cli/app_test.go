package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/KoukeNeko/aihki/internal/completioncache"
	"github.com/KoukeNeko/aihki/internal/config"
	"github.com/KoukeNeko/aihki/internal/credential"
	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

type fakeCredentials struct {
	values map[string]credential.Tokens
}

func (f *fakeCredentials) Get(account string) (credential.Tokens, error) {
	value, ok := f.values[account]
	if !ok {
		return credential.Tokens{}, credential.ErrNotFound
	}
	return value, nil
}

func (f *fakeCredentials) Set(account string, tokens credential.Tokens) error {
	f.values[account] = tokens
	return nil
}

func (f *fakeCredentials) Delete(account string) error {
	delete(f.values, account)
	return nil
}

func testApp(t *testing.T, server *httptest.Server) (*App, *bytes.Buffer, *bytes.Buffer, *fakeCredentials) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	apiURL := "https://example.invalid/api/v1/"
	client := http.DefaultClient
	if server != nil {
		apiURL = server.URL + "/api/v1/"
		client = server.Client()
	}
	store := config.NewStore(configPath)
	if err := store.Save(config.File{CurrentProfile: "test", Profiles: map[string]config.Profile{"test": {APIURL: apiURL, Project: "demo"}}}); err != nil {
		t.Fatal(err)
	}
	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	credentials := &fakeCredentials{values: map[string]credential.Tokens{credential.Account("test", apiURL): {AuthToken: "test-token"}}}
	app := &App{
		In:              strings.NewReader(""),
		Out:             out,
		Err:             stderr,
		HTTPClient:      client,
		Config:          store,
		GitLocal:        config.NewGitLocal(dir),
		Credentials:     credentials,
		CompletionCache: completioncache.NewStore(filepath.Join(dir, "completion-cache.json")),
		Getenv:          func(string) string { return "" },
		Cwd:             dir,
	}
	return app, out, stderr, credentials
}

func TestDynamicCompletionUsesFreshCache(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case "/api/v1/epics/by_ref":
			_, _ = io.WriteString(w, `{"id":15,"ref":8,"project":1,"subject":"Epic","description":"Body","version":2,"status":21,"status_extra_info":{"name":"New"}}`)
		case "/api/v1/epics":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":15,"ref":8,"project":1,"subject":"Epic","version":2,"status":21,"status_extra_info":{"name":"New"}}]`)
		case "/api/v1/epic-statuses":
			_, _ = io.WriteString(w, `[{"id":21,"name":"New","is_closed":false,"order":1},{"id":22,"name":"Closed","is_closed":true,"order":2}]`)
		case "/api/v1/epics/15/related_userstories":
			_, _ = io.WriteString(w, `[]`)
		case "/api/v1/issues":
			_, _ = io.WriteString(w, `[{"id":2,"ref":3,"project":1,"subject":"Cached issue","version":1}]`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, _, _, _ := testApp(t, server)
	command := &cobra.Command{}
	command.SetContext(context.Background())
	for attempt := 0; attempt < 2; attempt++ {
		values, directive := app.completeIssues(command, nil, "")
		if len(values) != 1 || !strings.HasPrefix(values[0], "3\tCached issue") || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("attempt=%d values=%#v directive=%v", attempt, values, directive)
		}
	}
	if requests != 2 {
		t.Fatalf("requests=%d, want one project and one issue request", requests)
	}
	data, err := os.ReadFile(filepath.Join(app.Cwd, "completion-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-token") {
		t.Fatalf("completion cache leaked credential: %s", data)
	}
}

func TestDynamicCompletionFallsBackToStaleCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"detail":"offline"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()
	app, _, _, _ := testApp(t, server)
	key := completioncache.Key("test", server.URL+"/api/v1/", "demo", "issues")
	payload, err := json.Marshal(map[string]any{
		"version": 1,
		"entries": map[string]any{
			key: map[string]any{"updated_at": time.Now().Add(-completioncache.FreshTTL - time.Minute).UTC(), "values": []string{"9\tStale issue"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Cwd, "completion-cache.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetContext(context.Background())
	values, _ := app.completeIssues(command, nil, "")
	if len(values) != 1 || values[0] != "9\tStale issue" {
		t.Fatalf("values=%#v", values)
	}
}

func TestSchemaMachineContract(t *testing.T) {
	app, out, stderr, _ := testApp(t, nil)
	if code := app.Execute(context.Background(), []string{"schema", "issue", "view", "--json"}); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["command"] != "issue view" || data["input_schema"] == nil {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestJSONErrorUsesStderrAndStableExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects/by_slug" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"detail":"project missing"}`)
			return
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "issue", "view", "1"})
	if code != ExitNotFound {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not JSON: %v: %s", err, stderr.String())
	}
	errorBody := envelope["error"].(map[string]any)
	if errorBody["code"] != "not_found" {
		t.Fatalf("error = %#v", errorBody)
	}
}

func TestDryRunPerformsNoMutation(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/issues/by_ref":
			_, _ = io.WriteString(w, `{"id":2,"ref":3,"project":1,"subject":"Old","version":7,"status_extra_info":{"name":"New"},"priority_extra_info":{"name":"Normal"},"severity_extra_info":{"name":"Normal"},"type_extra_info":{"name":"Bug"}}`)
		case r.Method == http.MethodPatch:
			patches++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "issue", "edit", "3", "--subject", "New", "--dry-run"})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if patches != 0 {
		t.Fatalf("PATCH requests = %d", patches)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	plan := envelope["plan"].(map[string]any)
	if plan["performed"] != false || plan["would_write"] != true {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestTokenLoginSavesKeyringEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me" || r.Header.Get("Authorization") != "Bearer supplied-token" {
			t.Fatalf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"id":4,"username":"smoke","full_name_display":"Smoke"}`)
	}))
	defer server.Close()
	app, out, stderr, credentials := testApp(t, server)
	app.In = strings.NewReader("supplied-token\n")
	code := app.Execute(context.Background(), []string{"--profile", "saved", "--api-url", server.URL + "/api/v1/", "--json", "auth", "login", "--with-token"})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	account := credential.Account("saved", server.URL+"/api/v1/")
	if credentials.values[account].AuthToken != "supplied-token" {
		t.Fatalf("credential missing: %#v, stdout=%s", credentials.values, out.String())
	}
}

func TestGitLocalSettingsOverrideProfile(t *testing.T) {
	app, _, _, _ := testApp(t, nil)
	cmd := exec.Command("git", "init", "-q", app.Cwd)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := app.GitLocal.Set(context.Background(), "profile", "local"); err != nil {
		t.Fatal(err)
	}
	if err := app.GitLocal.Set(context.Background(), "project", "local-project"); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.Config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Profiles["local"] = config.Profile{APIURL: "https://local.example/api/v1/"}
	if err := app.Config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	settings, _, err := app.resolveSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.Profile != "local" || settings.Project != "local-project" || settings.APIURL != "https://local.example/api/v1/" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestFieldsWithoutJSONIsUsageError(t *testing.T) {
	app, _, stderr, _ := testApp(t, nil)
	if code := app.Execute(context.Background(), []string{"--fields", "id", "schema", "issue", "view"}); code != ExitUsage {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
}

func TestStoryAndTaskCommandReadAndDryRunContracts(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.URL.Path {
		case "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case "/api/v1/epics/by_ref":
			_, _ = io.WriteString(w, `{"id":15,"ref":8,"project":1,"subject":"Epic","description":"Body","version":2,"status":21,"status_extra_info":{"name":"New"}}`)
		case "/api/v1/epics":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":15,"ref":8,"project":1,"subject":"Epic","version":2,"status":21,"status_extra_info":{"name":"New"}}]`)
		case "/api/v1/epic-statuses":
			_, _ = io.WriteString(w, `[{"id":21,"name":"New","is_closed":false,"order":1},{"id":22,"name":"Closed","is_closed":true,"order":2}]`)
		case "/api/v1/epics/15/related_userstories":
			_, _ = io.WriteString(w, `[]`)
		case "/api/v1/issues/by_ref":
			_, _ = io.WriteString(w, `{"id":2,"ref":3,"project":1,"subject":"Issue","description":"Body","version":1}`)
		case "/api/v1/userstories/by_ref":
			_, _ = io.WriteString(w, `{"id":2,"ref":3,"project":1,"subject":"Story","description":"Body","version":7,"status":4,"status_extra_info":{"name":"New"},"assigned_users":[],"is_closed":false}`)
		case "/api/v1/userstories":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":2,"ref":3,"project":1,"subject":"Story","version":7,"status":4,"status_extra_info":{"name":"New"}}]`)
		case "/api/v1/userstory-statuses":
			_, _ = io.WriteString(w, `[{"id":4,"name":"New","is_closed":false,"order":1},{"id":5,"name":"Closed","is_closed":true,"order":2}]`)
		case "/api/v1/milestones":
			_, _ = io.WriteString(w, `[{"id":6,"name":"Sprint 1","slug":"sprint-1","project":1,"estimated_start":"2026-08-31","estimated_finish":"2026-09-07","closed":false}]`)
		case "/api/v1/milestones/6":
			_, _ = io.WriteString(w, `{"id":6,"name":"Sprint 1","slug":"sprint-1","project":1,"estimated_start":"2026-08-31","estimated_finish":"2026-09-07","closed":false}`)
		case "/api/v1/users":
			_, _ = io.WriteString(w, `[{"id":8,"username":"demo","full_name_display":"Demo User"}]`)
		case "/api/v1/tasks/by_ref":
			_, _ = io.WriteString(w, `{"id":9,"ref":10,"project":1,"user_story":2,"user_story_extra_info":{"id":2,"ref":3,"subject":"Story"},"subject":"Task","description":"Body","version":4,"status":11,"status_extra_info":{"name":"New"},"is_closed":false}`)
		case "/api/v1/tasks":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":9,"ref":10,"project":1,"user_story":2,"user_story_extra_info":{"id":2,"ref":3,"subject":"Story"},"subject":"Task","version":4,"status":11,"status_extra_info":{"name":"New"},"is_closed":false}]`)
		case "/api/v1/task-statuses":
			_, _ = io.WriteString(w, `[{"id":11,"name":"New","is_closed":false,"order":1},{"id":12,"name":"Closed","is_closed":true,"order":2}]`)
		case "/api/v1/issues/attachments":
			_, _ = io.WriteString(w, `[{"id":13,"project":1,"object_id":2,"name":"note.txt","size":5}]`)
		case "/api/v1/issues/attachments/13":
			_, _ = io.WriteString(w, `{"id":13,"project":1,"object_id":2,"name":"note.txt","size":5}`)
		case "/api/v1/epics/attachments":
			_, _ = io.WriteString(w, `[{"id":16,"project":1,"object_id":15,"name":"epic.txt","size":5}]`)
		case "/api/v1/wiki/attachments":
			_, _ = io.WriteString(w, `[{"id":17,"project":1,"object_id":14,"name":"wiki.txt","size":5}]`)
		case "/api/v1/history/issue/2", "/api/v1/history/userstory/2", "/api/v1/history/task/9", "/api/v1/history/epic/15":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":"entry-1","created_at":"2026-08-31T00:00:00Z","type":1,"user":{"pk":8,"username":"demo","name":"Demo User"},"comment":"note"}]`)
		case "/api/v1/wiki":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":14,"project":1,"slug":"guide","content":"Hello","version":2,"editions":2}]`)
		case "/api/v1/wiki/by_slug":
			_, _ = io.WriteString(w, `{"id":14,"project":1,"slug":"guide","content":"Hello","version":2,"editions":2}`)
		case "/api/v1/history/wiki/14":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":"wiki-entry","created_at":"2026-08-31T00:00:00Z","type":1,"user":{"pk":8,"username":"demo","name":"Demo User"},"values_diff":{"content":["Old","Hello"]}}]`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{"--json", "story", "list"},
		{"--json", "story", "view", "3"},
		{"--json", "story", "create", "--subject", "New story", "--dry-run"},
		{"--json", "story", "edit", "3", "--subject", "Updated", "--dry-run"},
		{"--json", "story", "close", "3", "--status", "Closed", "--dry-run"},
		{"--json", "story", "move", "3", "--sprint", "sprint-1", "--dry-run"},
		{"--json", "story", "assign", "3", "--to", "demo", "--dry-run"},
		{"--json", "story", "comment", "3", "--body", "note", "--dry-run"},
		{"--json", "task", "list", "--story", "3"},
		{"--json", "task", "view", "10"},
		{"--json", "task", "create", "--subject", "New task", "--story", "3", "--dry-run"},
		{"--json", "task", "edit", "10", "--subject", "Updated", "--dry-run"},
		{"--json", "task", "done", "10", "--status", "Closed", "--dry-run"},
		{"--json", "task", "reopen", "10", "--status", "New", "--dry-run"},
		{"--json", "task", "assign", "10", "--to", "demo", "--dry-run"},
		{"--json", "task", "unassign", "10", "--dry-run"},
		{"--json", "task", "move", "10", "--story", "3", "--dry-run"},
		{"--json", "task", "move", "10", "--sprint", "sprint-1", "--dry-run"},
		{"--json", "task", "move", "10", "--sprint", "backlog", "--dry-run"},
		{"--json", "task", "comment", "10", "--body", "note", "--dry-run"},
		{"--json", "sprint", "list"},
		{"--json", "sprint", "view", "sprint-1"},
		{"--json", "sprint", "create", "--name", "Sprint 2", "--start", "2026-09-08", "--finish", "2026-09-14", "--dry-run"},
		{"--json", "sprint", "edit", "sprint-1", "--finish", "2026-09-08", "--dry-run"},
		{"--json", "sprint", "close", "sprint-1", "--dry-run"},
		{"--json", "sprint", "reopen", "sprint-1", "--dry-run"},
		{"--json", "attachment", "list", "issue", "3"},
		{"--json", "attachment", "view", "issue", "13"},
		{"--json", "attachment", "add", "issue", "3", "-", "--name", "note.txt", "--dry-run"},
		{"--json", "attachment", "edit", "issue", "13", "--description", "updated", "--dry-run"},
		{"--json", "attachment", "delete", "issue", "13", "--dry-run"},
		{"--json", "attachment", "list", "epic", "8"},
		{"--json", "attachment", "add", "epic", "8", "-", "--name", "epic.txt", "--dry-run"},
		{"--json", "attachment", "list", "wiki", "guide"},
		{"--json", "attachment", "add", "wiki", "guide", "-", "--name", "wiki.txt", "--dry-run"},
		{"--json", "issue", "watch", "3", "--dry-run"},
		{"--json", "issue", "unwatch", "3", "--dry-run"},
		{"--json", "issue", "vote", "3", "--dry-run"},
		{"--json", "issue", "unvote", "3", "--dry-run"},
		{"--json", "issue", "history", "3", "--type", "comment"},
		{"--json", "story", "watch", "3", "--dry-run"},
		{"--json", "story", "unwatch", "3", "--dry-run"},
		{"--json", "story", "vote", "3", "--dry-run"},
		{"--json", "story", "unvote", "3", "--dry-run"},
		{"--json", "story", "history", "3", "--type", "activity"},
		{"--json", "task", "watch", "10", "--dry-run"},
		{"--json", "task", "unwatch", "10", "--dry-run"},
		{"--json", "task", "vote", "10", "--dry-run"},
		{"--json", "task", "unvote", "10", "--dry-run"},
		{"--json", "task", "history", "10"},
		{"--json", "wiki", "list"},
		{"--json", "wiki", "view", "guide"},
		{"--json", "wiki", "create", "--slug", "new-page", "--body", "Hello", "--dry-run"},
		{"--json", "wiki", "edit", "guide", "--body", "Updated", "--dry-run"},
		{"--json", "wiki", "delete", "guide", "--dry-run"},
		{"--json", "wiki", "watch", "guide", "--dry-run"},
		{"--json", "wiki", "unwatch", "guide", "--dry-run"},
		{"--json", "wiki", "history", "guide", "--type", "activity"},
		{"--json", "epic", "list"},
		{"--json", "epic", "view", "8"},
		{"--json", "epic", "create", "--subject", "New epic", "--dry-run"},
		{"--json", "epic", "edit", "8", "--subject", "Updated", "--dry-run"},
		{"--json", "epic", "close", "8", "--status", "Closed", "--dry-run"},
		{"--json", "epic", "stories", "8"},
		{"--json", "epic", "link", "8", "--story", "3", "--dry-run"},
		{"--json", "epic", "unlink", "8", "--story", "3", "--dry-run"},
		{"--json", "epic", "watch", "8", "--dry-run"},
		{"--json", "epic", "unwatch", "8", "--dry-run"},
		{"--json", "epic", "vote", "8", "--dry-run"},
		{"--json", "epic", "unvote", "8", "--dry-run"},
		{"--json", "epic", "history", "8", "--type", "activity"},
	}
	for _, args := range commands {
		app, out, stderr, _ := testApp(t, server)
		if code := app.Execute(context.Background(), args); code != ExitSuccess {
			t.Fatalf("aihki %v exit=%d stderr=%s", args, code, stderr.String())
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("aihki %v returned invalid JSON: %s", args, out.String())
		}
	}
	if mutations != 0 {
		t.Fatalf("dry-run/read commands sent %d mutation requests", mutations)
	}
}

func TestWatchCommandsVerifyServerState(t *testing.T) {
	watching := false
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/issues/by_ref":
			_, _ = fmt.Fprintf(w, `{"id":2,"ref":3,"project":1,"subject":"Issue","version":1,"is_watcher":%t}`, watching)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/issues/2/watch":
			posts++
			watching = true
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/issues/2/unwatch":
			posts++
			watching = false
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/issues/2":
			_, _ = fmt.Fprintf(w, `{"is_watcher":%t}`, watching)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, test := range []struct {
		command  string
		expected bool
	}{
		{command: "watch", expected: true},
		{command: "unwatch", expected: false},
	} {
		app, out, stderr, _ := testApp(t, server)
		code := app.Execute(context.Background(), []string{"--json", "issue", test.command, "3"})
		if code != ExitSuccess {
			t.Fatalf("%s exit=%d stderr=%s", test.command, code, stderr.String())
		}
		var envelope struct {
			Data watchView `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Watching != test.expected || !envelope.Data.Verified {
			t.Fatalf("%s data=%#v", test.command, envelope.Data)
		}
	}
	if posts != 2 {
		t.Fatalf("posts=%d", posts)
	}
}

func TestVoteCommandsVerifyServerState(t *testing.T) {
	voting := false
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/issues/by_ref":
			_, _ = fmt.Fprintf(w, `{"id":2,"ref":3,"project":1,"subject":"Issue","version":1,"is_voter":%t}`, voting)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/issues/2/upvote":
			posts++
			voting = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/issues/2/downvote":
			posts++
			voting = false
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/issues/2":
			_, _ = fmt.Fprintf(w, `{"is_voter":%t}`, voting)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, test := range []struct {
		command  string
		expected bool
	}{
		{command: "vote", expected: true},
		{command: "vote", expected: true},
		{command: "unvote", expected: false},
	} {
		app, out, stderr, _ := testApp(t, server)
		code := app.Execute(context.Background(), []string{"--json", "issue", test.command, "3"})
		if code != ExitSuccess {
			t.Fatalf("%s exit=%d stderr=%s", test.command, code, stderr.String())
		}
		var envelope struct {
			Data voteView `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Voting != test.expected || !envelope.Data.Verified {
			t.Fatalf("%s data=%#v", test.command, envelope.Data)
		}
	}
	if posts != 2 {
		t.Fatalf("posts=%d, want idempotent commands to send two mutations", posts)
	}
}

func TestAttachmentDeleteRequiresConfirmation(t *testing.T) {
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/api/v1/issues/attachments/13" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":13,"project":1,"object_id":2,"name":"note.txt","size":5}`)
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "--no-input", "attachment", "delete", "issue", "13"})
	if code != ExitConfirmationRequired || out.Len() != 0 || deletes != 0 || !strings.Contains(stderr.String(), "confirmation_required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s deletes=%d", code, out.String(), stderr.String(), deletes)
	}
}

func TestWikiDeleteRequiresConfirmation(t *testing.T) {
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/wiki/by_slug":
			_, _ = io.WriteString(w, `{"id":14,"project":1,"slug":"guide","content":"Hello","version":2}`)
		case r.Method == http.MethodDelete:
			deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "--no-input", "wiki", "delete", "guide"})
	if code != ExitConfirmationRequired || out.Len() != 0 || deletes != 0 || !strings.Contains(stderr.String(), "confirmation_required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s deletes=%d", code, out.String(), stderr.String(), deletes)
	}
}

func TestProjectDeleteRequiresConfirmation(t *testing.T) {
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo","is_private":true}`)
		case r.Method == http.MethodDelete:
			deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "--no-input", "project", "delete", "demo"})
	if code != ExitConfirmationRequired || out.Len() != 0 || deletes != 0 || !strings.Contains(stderr.String(), "confirmation_required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s deletes=%d", code, out.String(), stderr.String(), deletes)
	}
}

func TestProjectArchiveReportsUnsupportedCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects/by_slug" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "project", "archive", "demo"})
	if code != ExitValidation || out.Len() != 0 || !strings.Contains(stderr.String(), "unsupported_capability") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestWebhookOutputDoesNotLeakSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/webhooks":
			_, _ = io.WriteString(w, `{"id":3,"project":1,"name":"CI","url":"https://example.test/hook","key":"must-not-leak"}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	code := app.Execute(context.Background(), []string{"--json", "webhook", "create", "--name", "CI", "--url", "https://example.test/hook", "--secret", "must-not-leak"})
	if code != ExitSuccess || strings.Contains(out.String()+stderr.String(), "must-not-leak") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestSprintCreateRejectsInvalidDateRange(t *testing.T) {
	app, out, stderr, _ := testApp(t, nil)
	code := app.Execute(context.Background(), []string{"--json", "sprint", "create", "--name", "Bad Sprint", "--start", "2026-09-10", "--finish", "2026-09-01"})
	if code != ExitValidation || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid_date_range") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestDynamicProjectCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":1,"name":"Demo Project","slug":"demo"},{"id":2,"name":"Other","slug":"other"}]`)
	}))
	defer server.Close()
	app, _, _, _ := testApp(t, server)
	command := app.rootCommand()
	command.SetContext(context.Background())
	values, directive := app.completeProjects(command, nil, "de")
	if directive != cobra.ShellCompDirectiveNoFileComp || len(values) != 1 || values[0] != "demo\tDemo Project" {
		t.Fatalf("values=%#v directive=%v", values, directive)
	}
}

func TestFlattenSearchFiltersAndLimits(t *testing.T) {
	response := taiga.SearchResponse{
		UserStories: []taiga.SearchItem{{ID: 1, Ref: 2, Subject: "Story"}},
		Tasks:       []taiga.SearchItem{{ID: 3, Ref: 4, Subject: "Task"}},
		Issues:      []taiga.SearchItem{{ID: 5, Ref: 6, Subject: "Issue"}},
		Count:       3,
	}
	items := flattenSearch(response, "all", 2)
	if len(items) != 2 || items[0].Kind != "story" || items[1].Kind != "task" {
		t.Fatalf("items = %#v", items)
	}
	issues := flattenSearch(response, "issue", 10)
	if len(issues) != 1 || issues[0].Subject != "Issue" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestStoryListRejectsUnknownOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects/by_slug" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"--json", "story", "list", "--order-by", "unknown"}); code != ExitUsage {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

// CI jobs and shells still export the pre-rename variables, so those must keep
// working, while the current names take precedence when both are set.
// The client the CLI builds carries no overall deadline: that would cap every
// attachment and dump at whatever thirty seconds can move.
func TestNewClientCarriesNoOverallDeadline(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.HTTPClient.Timeout != 0 {
		t.Fatalf("HTTPClient.Timeout = %v, want none", app.HTTPClient.Timeout)
	}
}

func TestEnvironmentFallsBackToTheLegacyPrefix(t *testing.T) {
	values := map[string]string{}
	app := &App{Getenv: func(name string) string { return values[name] }}

	values[legacyEnvironmentPrefix+"TOKEN"] = "legacy-token"
	if got := app.env("TOKEN"); got != "legacy-token" {
		t.Fatalf("env(TOKEN) = %q, want the legacy value", got)
	}

	values[environmentPrefix+"TOKEN"] = "current-token"
	if got := app.env("TOKEN"); got != "current-token" {
		t.Fatalf("env(TOKEN) = %q, want the current value to win", got)
	}

	values[environmentPrefix+"TOKEN"] = "   "
	if got := app.env("TOKEN"); got != "legacy-token" {
		t.Fatalf("env(TOKEN) = %q, want a blank current value to fall through", got)
	}

	if got := app.env("PROJECT"); got != "" {
		t.Fatalf("env(PROJECT) = %q, want empty", got)
	}
}

// A table that stops at the page size looks exactly like a list that ends
// there, so a page holding only part of the list has to say so. The notice
// goes to stderr, leaving the table itself pipeable.
func TestFlushTableSaysWhenThePageIsPartial(t *testing.T) {
	for name, tc := range map[string]struct {
		shown, total int
		quiet        bool
		wantNote     string
	}{
		"partial":        {shown: 30, total: 54, wantNote: "showing 30 of 54; use --limit to see more\n"},
		"whole list":     {shown: 16, total: 16, wantNote: ""},
		"total unknown":  {shown: 12, total: 0, wantNote: ""},
		"quiet is quiet": {shown: 30, total: 54, quiet: true, wantNote: ""},
	} {
		t.Run(name, func(t *testing.T) {
			out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			app := &App{Out: out, Err: stderr}
			app.global.Quiet = tc.quiet
			writer := tabwriter.NewWriter(app.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "REF\tSUBJECT")
			if err := app.flushTable(writer, tc.shown, tc.total); err != nil {
				t.Fatal(err)
			}
			if stderr.String() != tc.wantNote {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.wantNote)
			}
			if !strings.Contains(out.String(), "REF") {
				t.Errorf("stdout = %q, want the table flushed to it", out.String())
			}
		})
	}
}

// The notice reports what the server said the list holds, not what the page
// happens to carry, which is the whole point of showing it.
func TestListCommandReportsTheServerTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case "/api/v1/userstories":
			w.Header().Set("X-Pagination-Current", "1")
			w.Header().Set("X-Paginated-By", "2")
			w.Header().Set("X-Pagination-Count", "54")
			_, _ = io.WriteString(w, `[{"id":1,"ref":76,"subject":"one","version":1},{"id":2,"ref":77,"subject":"two","version":1}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"story", "list", "--project", "demo"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "showing 2 of 54") {
		t.Errorf("stderr = %q, want the server's total", stderr.String())
	}
	if strings.Contains(out.String(), "showing") {
		t.Errorf("stdout = %q, want the notice kept off it", out.String())
	}
}
