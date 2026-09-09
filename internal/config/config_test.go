package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	store := NewStore(path)
	want := File{CurrentProfile: "local", Profiles: map[string]Profile{"local": {APIURL: "http://localhost/api/v1/", Project: "demo"}}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentProfile != "local" || got.Profiles["local"].Project != "demo" {
		t.Fatalf("config = %#v", got)
	}
	want.Profiles["local"] = Profile{APIURL: "http://localhost/api/v1/", Project: "changed"}
	if err := store.Save(want); err != nil {
		t.Fatalf("replace config: %v", err)
	}
	got, err = store.Load()
	if err != nil || got.Profiles["local"].Project != "changed" {
		t.Fatalf("reloaded config = %#v, error = %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestGitLocal(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	local := NewGitLocal(dir)
	ctx := context.Background()
	if err := local.Set(ctx, "profile", "local"); err != nil {
		t.Fatal(err)
	}
	if err := local.Set(ctx, "project", "demo"); err != nil {
		t.Fatal(err)
	}
	values, err := local.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if values.Profile != "local" || values.Project != "demo" {
		t.Fatalf("values = %#v", values)
	}
}

// A parse error has to name the file that was read so the fix goes to the
// right place rather than to a path that was never touched.
func TestLoadNamesAMalformedFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, configDirectory, "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current_profile = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(path).Load()
	if err == nil {
		t.Fatal("a malformed config must not load")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name %q", err, path)
	}
}
