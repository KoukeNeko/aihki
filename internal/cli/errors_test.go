package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/KoukeNeko/aihki/internal/taiga"
)

// An interrupted write reports that something may have been committed. The
// cancellation that caused it is still reachable through the error chain, so
// the order these are tested in decides whether that warning survives.
func TestClassifyErrorKeepsAnUnconfirmedWriteAheadOfItsCancellation(t *testing.T) {
	unconfirmed := &taiga.Error{
		Kind:    taiga.KindAmbiguousCommit,
		Message: "request was interrupted before Taiga confirmed it; verify before retrying",
		Cause:   context.Canceled,
	}
	if !errors.Is(unconfirmed, context.Canceled) {
		t.Fatal("this test is pointless unless the error really does wrap the cancellation")
	}
	known, body := classifyError(unconfirmed)
	if known.ExitCode != ExitAmbiguousCommit {
		t.Errorf("exit code = %d, want %d", known.ExitCode, ExitAmbiguousCommit)
	}
	if body.Code != string(taiga.KindAmbiguousCommit) {
		t.Errorf("code = %q, want %q", body.Code, taiga.KindAmbiguousCommit)
	}
}

// A cancellation carrying nothing else is the operator's own decision, not a
// defect in the command.
func TestClassifyErrorReportsAPlainInterruptAsSuch(t *testing.T) {
	for _, testCase := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "interrupted"},
		{context.DeadlineExceeded, "timeout"},
		{fmt.Errorf("fetching issues: %w", context.Canceled), "interrupted"},
	} {
		known, body := classifyError(testCase.err)
		if known.ExitCode != ExitInterrupted {
			t.Errorf("%v: exit code = %d, want %d", testCase.err, known.ExitCode, ExitInterrupted)
		}
		if body.Code != testCase.code {
			t.Errorf("%v: code = %q, want %q", testCase.err, body.Code, testCase.code)
		}
	}
}

// An argument count on its own says nothing about what to pass, which leaves
// a person reading help a second time and an agent retrying the same call.
// The Use line already names the arguments, so the error carries it.
func TestArgumentErrorsNameWhatWasExpected(t *testing.T) {
	for name, tc := range map[string]struct {
		argv []string
		want string
	}{
		"missing positional": {
			argv: []string{"custom-field", "list"},
			want: "usage: aihki custom-field list <epic|story|task|issue>",
		},
		"positional given as a flag": {
			argv: []string{"project", "view"},
			want: "usage: aihki project view <slug>",
		},
		"too many": {
			argv: []string{"stats", "project", "one", "two"},
			want: "usage: aihki stats project",
		},
	} {
		t.Run(name, func(t *testing.T) {
			app, _, stderr, _ := testApp(t, nil)
			if code := app.Execute(context.Background(), tc.argv); code != ExitUsage {
				t.Fatalf("exit=%d, want %d; stderr=%s", code, ExitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.want)
			}
		})
	}
}

// The complete field list is one bad --fields away, and it is the only
// authoritative one: a field the sampled row left out still appears. Nothing
// says so unless the flag's own help does.
func TestFieldsFlagAdvertisesItsOwnDiscovery(t *testing.T) {
	app, out, _, _ := testApp(t, nil)
	if code := app.Execute(context.Background(), []string{"--help"}); code != ExitSuccess {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out.String(), "unknown name") {
		t.Errorf("--fields help = %q, want it to say an unknown name lists them", out.String())
	}
}

// An agent reads --help, not the README, and schema is one alphabetical entry
// among thirty-odd commands with nothing marking it as the one that describes
// the rest.
func TestRootHelpPointsAnUnattendedCallerAtSchema(t *testing.T) {
	app, out, _, _ := testApp(t, nil)
	if code := app.Execute(context.Background(), []string{"--help"}); code != ExitSuccess {
		t.Fatalf("exit=%d", code)
	}
	for _, want := range []string{"aihki schema", "--json"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("root help = %q, want it to mention %q", out.String(), want)
		}
	}
}
