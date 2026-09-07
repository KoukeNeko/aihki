package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mapOutputCommands emit rows built on the spot rather than from a type, so
// there is nothing to describe them from. They are named here so that a new
// list command cannot join them by accident.
var mapOutputCommands = map[string]bool{"tag list": true, "integration providers": true}

// A list schema that stops at "array" tells a caller the envelope and nothing
// about what is in it, which leaves the field names to be guessed or probed.
func TestEveryListCommandDescribesItsItems(t *testing.T) {
	for name, descriptor := range descriptors() {
		properties, _ := descriptor.Output["properties"].(map[string]any)
		items, ok := properties["items"].(map[string]any)
		if !ok {
			continue // not a list command
		}
		_, described := items["items"]
		if described == mapOutputCommands[name] {
			t.Errorf("%s: item shape described = %t, want %t", name, described, !mapOutputCommands[name])
		}
	}
}

// omitempty is the difference between a field that is missing from one row and
// a field that does not exist. Reading the first row cannot tell them apart;
// the schema can, and must.
func TestSchemaMarksOmitEmptyFieldsOptional(t *testing.T) {
	schema := schemaOf(historyView{})
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["comment"]; !ok {
		t.Fatal("comment is absent from the schema although the type declares it")
	}
	required := map[string]bool{}
	for _, name := range schema["required"].([]string) {
		required[name] = true
	}
	if required["comment"] {
		t.Error("comment is omitempty, so it must not be required")
	}
	for _, name := range []string{"created_at", "kind", "author"} {
		if !required[name] {
			t.Errorf("%s is always written, so it must be required", name)
		}
	}
}

// The names in the schema are the names the encoder writes, not the Go field
// names and not the table's column headings.
func TestSchemaUsesTheEncodedNames(t *testing.T) {
	properties := schemaOf(storyView{})["properties"].(map[string]any)
	for name, want := range map[string]string{"total_points": "number", "points": "object", "created_date": "string", "assigned_users": "array"} {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Errorf("%s is absent", name)
			continue
		}
		if property["type"] != want {
			t.Errorf("%s: type = %v, want %s", name, property["type"], want)
		}
	}
}

// The described shape has to be the shape the command actually emits, which
// only running one can show.
func TestDescribedShapeMatchesWhatAListEmits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/by_slug":
			_, _ = io.WriteString(w, `{"id":1,"name":"Demo","slug":"demo"}`)
		case "/api/v1/userstories":
			w.Header().Set("X-Pagination-Count", "1")
			_, _ = io.WriteString(w, `[{"id":1,"ref":76,"subject":"one","version":1,"total_points":3,"points":{"21":37},"assigned_users":[5],"status_extra_info":{"name":"New"}}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	app, out, stderr, _ := testApp(t, server)
	if code := app.Execute(context.Background(), []string{"--json", "story", "list", "--project", "demo"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(envelope.Items))
	}
	described := descriptors()["story list"].Output["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)
	properties := described["properties"].(map[string]any)
	for field := range envelope.Items[0] {
		if _, ok := properties[field]; !ok {
			t.Errorf("story list emits %q, which its schema does not describe", field)
		}
	}
	for _, field := range described["required"].([]string) {
		if _, ok := envelope.Items[0][field]; !ok {
			t.Errorf("schema requires %q, which story list did not emit", field)
		}
	}
}
