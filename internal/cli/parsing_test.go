package cli

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// parseBatchOrders reads what a person typed on the command line and decides
// which records get reordered, so every way of getting it wrong is worth
// stating.
func TestParseBatchOrders(t *testing.T) {
	accepted := map[string]map[int64]int{
		"1=0":     {1: 0},
		"12=3":    {12: 3},
		" 7 = 2 ": {7: 2},
	}
	for input, want := range accepted {
		got, err := parseBatchOrders([]string{input})
		if err != nil {
			t.Errorf("%q: unexpected error %v", input, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q = %v, want %v", input, got, want)
		}
	}

	got, err := parseBatchOrders([]string{"4=1", "5=2"})
	if err != nil || !reflect.DeepEqual(got, map[int64]int{4: 1, 5: 2}) {
		t.Errorf("two entries = %v, %v", got, err)
	}

	rejected := map[string][]string{
		"no values":             {},
		"no separator":          {"41"},
		"missing order":         {"4="},
		"missing id":            {"=2"},
		"id is not a number":    {"four=1"},
		"order is not a number": {"4=one"},
		"zero id":               {"0=1"},
		"negative id":           {"-4=1"},
		"negative order":        {"4=-1"},
		"repeated id":           {"4=1", "4=2"},
	}
	for name, input := range rejected {
		if _, err := parseBatchOrders(input); err == nil {
			t.Errorf("%s: %v was accepted", name, input)
		}
	}

	// The cap exists so one command cannot reorder a whole project by accident.
	tooMany := make([]string, 0, maxBatchItems+1)
	for index := range maxBatchItems + 1 {
		tooMany = append(tooMany, strconv.Itoa(index+1)+"=0")
	}
	if _, err := parseBatchOrders(tooMany); err == nil {
		t.Errorf("%d entries was accepted, over the %d cap", len(tooMany), maxBatchItems)
	}
}

// An importer's response carries the credentials it was handed. None of them
// may reach output, including a dry run.
func TestRedactImporterResult(t *testing.T) {
	secret := "must-never-appear"
	fields := map[string]any{
		"token":         secret,
		"auth_token":    secret,
		"AUTH_CODE":     secret,
		"client_secret": secret,
		"Password":      secret,
		"code_verifier": secret,
		"project":       "visible",
		"count":         3,
	}
	redacted, ok := redactImporterResult(fields).(map[string]any)
	if !ok {
		t.Fatal("a map must come back as a map")
	}
	for key, value := range redacted {
		if value == secret {
			t.Errorf("%q still carries the credential", key)
		}
	}
	if redacted["project"] != "visible" || redacted["count"] != 3 {
		t.Errorf("non-secret fields were altered: %v", redacted)
	}
	if len(redacted) != len(fields) {
		t.Errorf("redaction dropped fields: %d of %d", len(redacted), len(fields))
	}
	// Anything that is not an object is passed through, since there are no
	// field names to judge.
	if got := redactImporterResult("plain"); got != "plain" {
		t.Errorf("non-object became %v", got)
	}
}

func TestPositiveID(t *testing.T) {
	for input, want := range map[string]int64{"1": 1, " 42 ": 42, "9007199254740993": 9007199254740993} {
		got, err := positiveID(input, "Project")
		if err != nil || got != want {
			t.Errorf("positiveID(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0", "-1", "1.5", "one", "1e3", " "} {
		if _, err := positiveID(input, "Project"); err == nil {
			t.Errorf("positiveID(%q) was accepted", input)
		} else if !strings.Contains(err.Error(), "Project") {
			t.Errorf("positiveID(%q) error omits the name: %v", input, err)
		}
	}
}

// The aliases exist so that a name a person is likely to type reaches the same
// resource as the name the API uses.
func TestNormaliseAliases(t *testing.T) {
	for input, want := range map[string]string{
		"userstory": "story", "user-story": "story", "us": "story",
		"wikipage": "wiki", "wiki-page": "wiki",
		"  ISSUE  ": "issue", "epic": "epic", "unknown": "unknown",
	} {
		if got := normalizeSearchKind(input); got != want {
			t.Errorf("normalizeSearchKind(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"current-profile": "profile", "current_profile": "profile",
		"api_url": "api-url", "apiurl": "api-url", "project": "project",
	} {
		if got := normalizeConfigKey(input); got != want {
			t.Errorf("normalizeConfigKey(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"story": "story", "userstory": "story", "user-story": "story",
		" TASK ": "task", "issue": "issue",
	} {
		got, err := normalizeDueDateResource(input)
		if err != nil || got != want {
			t.Errorf("normalizeDueDateResource(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"epic", "wiki", "", "sprint"} {
		if _, err := normalizeDueDateResource(input); err == nil {
			t.Errorf("normalizeDueDateResource(%q) was accepted", input)
		}
	}
}

func TestValidKinds(t *testing.T) {
	for _, kind := range []string{"all", "epic", "story", "task", "issue", "wiki"} {
		if !validSearchKind(kind) {
			t.Errorf("validSearchKind(%q) = false", kind)
		}
	}
	// validSearchKind runs on an already-normalised value, so an alias is not
	// one of its inputs and must not be accepted here.
	for _, kind := range []string{"userstory", "Story", "", "sprint"} {
		if validSearchKind(kind) {
			t.Errorf("validSearchKind(%q) = true", kind)
		}
	}
	for _, kind := range []string{"text", "multiline", "richtext", "date", "url", "dropdown", "checkbox", "number", " TEXT "} {
		if !validCustomFieldType(kind) {
			t.Errorf("validCustomFieldType(%q) = false", kind)
		}
	}
	for _, kind := range []string{"", "string", "boolean", "integer"} {
		if validCustomFieldType(kind) {
			t.Errorf("validCustomFieldType(%q) = true", kind)
		}
	}
}

// A custom field value is whatever the person typed. Anything that parses as
// JSON keeps its type so a number stays a number, and everything else is the
// string itself rather than an error.
func TestParseCustomFieldValue(t *testing.T) {
	cases := []struct {
		raw  string
		want any
	}{
		{"42", float64(42)},
		{"true", true},
		{"null", nil},
		{`"quoted"`, "quoted"},
		{"not json", "not json"},
		{"", ""},
		{"2026-09-03", "2026-09-03"},
	}
	for _, testCase := range cases {
		if got := parseCustomFieldValue(testCase.raw); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("parseCustomFieldValue(%q) = %#v, want %#v", testCase.raw, got, testCase.want)
		}
	}
	if got := parseCustomFieldValue(`{"a":1}`); !reflect.DeepEqual(got, map[string]any{"a": float64(1)}) {
		t.Errorf("an object was not preserved: %#v", got)
	}
}

func TestAuthRequiredIsAnAuthError(t *testing.T) {
	err := authRequired("run `taiga auth login` first")
	var known *contractError
	if !errors.As(err, &known) {
		t.Fatalf("authRequired returned %T, want a contract error", err)
	}
	if known.ExitCode != ExitAuth {
		t.Errorf("exit code = %d, want %d", known.ExitCode, ExitAuth)
	}
	if known.Code != "authentication_required" {
		t.Errorf("code = %q, want authentication_required", known.Code)
	}
	if !strings.Contains(known.Error(), "auth login") {
		t.Errorf("message = %q, want it to name the command to run", known.Error())
	}
}

// issue comment names the text a comment; story and task call it a body. The
// difference is only wording, but the code is what a script branches on, so
// merging the three commands onto one helper must not quietly unify it.
func TestReadBodyKeepsEachCommandsWording(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		wording bodyWording
		code    string
		message string
		context string
	}{
		{"story and task", genericBody, "empty_body", "body cannot be empty", "read body"},
		{"issue comment", commentBody, "empty_comment", "comment body cannot be empty", "read comment body"},
	} {
		for _, empty := range []struct{ body, file string }{{"", ""}, {"   ", ""}} {
			_, err := readBody(strings.NewReader(""), empty.body, empty.file, testCase.wording)
			var known *contractError
			if !errors.As(err, &known) {
				t.Fatalf("%s: got %v, want a contract error", testCase.name, err)
			}
			if known.Code != testCase.code || known.Message != testCase.message {
				t.Errorf("%s: %q / %q, want %q / %q", testCase.name, known.Code, known.Message, testCase.code, testCase.message)
			}
		}
		// A file holding only whitespace is empty too, which is only knowable
		// after reading it.
		if _, err := readBody(strings.NewReader("  \n "), "", "-", testCase.wording); err == nil {
			t.Errorf("%s: a whitespace-only body was accepted", testCase.name)
		}
		_, err := readBody(strings.NewReader(""), "", "/nonexistent-body-file", testCase.wording)
		if err == nil || !strings.HasPrefix(err.Error(), testCase.context+":") {
			t.Errorf("%s: read error = %v, want it prefixed %q", testCase.name, err, testCase.context)
		}
	}
	if got, err := readBody(strings.NewReader("from stdin"), "", "-", genericBody); err != nil || got != "from stdin" {
		t.Errorf("stdin body = %q, %v", got, err)
	}
}

// Proving readBodyAs honours a wording says nothing about whether the issue
// command passes the right one. This drives the command itself, and reaches
// the body check before any request is made, so no server is needed.
func TestIssueCommentKeepsItsOwnEmptyCode(t *testing.T) {
	for _, testCase := range []struct {
		command func(*App) *cobra.Command
		name    string
		code    string
	}{
		{func(a *App) *cobra.Command { return a.issueCommentCommand() }, "issue", "empty_comment"},
		{func(a *App) *cobra.Command { return a.storyCommentCommand() }, "story", "empty_body"},
		{func(a *App) *cobra.Command { return a.taskCommentCommand() }, "task", "empty_body"},
	} {
		app, _, _, _ := testApp(t, nil)
		command := testCase.command(app)
		// Whitespace rather than empty: an entirely absent body is refused by
		// the flag check before the body is ever read.
		command.SetArgs([]string{"1", "--body", "   "})
		err := command.Execute()
		var known *contractError
		if !errors.As(err, &known) {
			t.Errorf("%s comment: got %v, want a contract error", testCase.name, err)
			continue
		}
		if known.Code != testCase.code {
			t.Errorf("%s comment: code = %q, want %q", testCase.name, known.Code, testCase.code)
		}
	}
}

// `comment edit` had a second empty-body check after readBody, which readBody
// had already made unreachable. Removing it is only safe if the code the
// command actually returns is the one readBody produces, so that is what this
// pins -- reached before any request, so no server is needed.
func TestCommentEditReturnsTheCodeReadBodyProduces(t *testing.T) {
	app, _, _, _ := testApp(t, nil)
	command := app.commentEditCommand()
	// Run on its own, the subcommand would print its usage on the error,
	// which the root command normally silences.
	command.SilenceUsage, command.SilenceErrors = true, true
	command.SetArgs([]string{"issue", "1", "2", "--body", "   "})
	err := command.Execute()
	var known *contractError
	if !errors.As(err, &known) {
		t.Fatalf("got %v, want a contract error", err)
	}
	if known.Code != "empty_body" {
		t.Errorf("code = %q, want %q", known.Code, "empty_body")
	}
}
