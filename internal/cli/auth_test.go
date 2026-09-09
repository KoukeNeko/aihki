package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/KoukeNeko/taiga-cli/internal/config"
	"github.com/KoukeNeko/taiga-cli/internal/credential"
)

// A script that forgot the URL is told what to pass, not asked a question it
// cannot answer.
func TestLoginRequiresAURLOffATerminal(t *testing.T) {
	app, _, stderr, _ := testApp(t, nil)
	if err := app.Config.Save(config.File{CurrentProfile: "test", Profiles: map[string]config.Profile{"test": {}}}); err != nil {
		t.Fatal(err)
	}
	app.In = strings.NewReader("token\n")
	if code := app.Execute(context.Background(), []string{"--json", "auth", "login", "--with-token"}); code != ExitValidation {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{`"code":"missing_api_url"`, "--url"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Errorf("stderr = %s, want it to contain %q", stderr.String(), expected)
		}
	}
}

// The URL people have is the page in front of them, and the flag that takes
// it is --url; --host keeps working for the scripts that learned it.
func TestLoginDiscoversTheAPIFromAnyPageUnderEitherFlagName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/conf.json":
			_, _ = io.WriteString(w, `{"api":"http://`+r.Host+`/api/v1/","baseHref":"/"}`)
		case "/api/v1/locales":
			_, _ = io.WriteString(w, `[{"code":"en","name":"English"}]`)
		case "/api/v1/users/me":
			if r.Header.Get("Authorization") != "Bearer pasted-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
		default:
			_, _ = io.WriteString(w, `<!doctype html><html><body>taiga</body></html>`)
		}
	}))
	defer server.Close()
	for _, flag := range []string{"--url", "--host"} {
		t.Run(flag, func(t *testing.T) {
			app, out, stderr, credentials := testApp(t, server)
			app.In = strings.NewReader("pasted-token\n")
			code := app.Execute(context.Background(), []string{"--profile", "site", "--json", "auth", "login", flag, server.URL + "/project/demo/backlog", "--with-token"})
			if code != ExitSuccess {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if !strings.Contains(out.String(), `"api_url":"`+server.URL+`/api/v1/"`) {
				t.Errorf("stdout = %s, want the discovered API", out.String())
			}
			if saved := credentials.values[credential.Account("site", server.URL+"/api/v1/")]; saved.AuthToken != "pasted-token" {
				t.Errorf("saved = %#v, want the pasted token under the discovered API", saved)
			}
		})
	}
}

// --with-token takes the web app's JSON object so that the refresh token comes
// along, and a bare token so that a script with only that still works.
func TestParseTokenInputTakesABareTokenOrTheWebAppsObject(t *testing.T) {
	for input, want := range map[string]credential.Tokens{
		"abc":                                  {AuthToken: "abc"},
		"  abc\n":                              {AuthToken: "abc"},
		`{"auth_token":"abc","refresh":"def"}`: {AuthToken: "abc", RefreshToken: "def"},
		`{"token":"abc","refresh":"def"}`:      {AuthToken: "abc", RefreshToken: "def"},
		`{"auth_token":"abc"}`:                 {AuthToken: "abc"},
		"{\"auth_token\": \"abc\", \"refresh\": \"def\"}\n": {AuthToken: "abc", RefreshToken: "def"},
	} {
		got, err := parseTokenInput(input)
		if err != nil || got != want {
			t.Errorf("input %q: got %+v, %v; want %+v", input, got, err, want)
		}
	}
	for _, input := range []string{`{"refresh":"def"}`, `{not json`} {
		if _, err := parseTokenInput(input); err == nil {
			t.Errorf("input %q was accepted", input)
		}
	}
}

// A refresh token pasted with the access token is stored beside it, which is
// what lets a token login renew itself the way a password login does.
func TestTokenLoginStoresThePastedRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/me" && r.Header.Get("Authorization") == "Bearer pasted-token" {
			_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	for input, wantRefresh := range map[string]string{
		`{"auth_token":"pasted-token","refresh":"pasted-refresh"}`: "pasted-refresh",
		"pasted-token": "",
	} {
		app, out, stderr, credentials := testApp(t, server)
		app.In = strings.NewReader(input + "\n")
		if code := app.Execute(context.Background(), []string{"--profile", "pasted", "--api-url", server.URL + "/api/v1/", "--json", "auth", "login", "--with-token"}); code != ExitSuccess {
			t.Fatalf("input %q: exit=%d stderr=%s", input, code, stderr.String())
		}
		saved := credentials.values[credential.Account("pasted", server.URL+"/api/v1/")]
		if saved.AuthToken != "pasted-token" || saved.RefreshToken != wantRefresh {
			t.Errorf("input %q: saved %+v, want refresh %q", input, saved, wantRefresh)
		}
		if !strings.Contains(out.String(), `"refresh_token_stored":`+strconv.FormatBool(wantRefresh != "")) {
			t.Errorf("input %q: stdout = %s", input, out.String())
		}
	}
}

// The access token copied from the browser may have expired by the time it is
// pasted. With the refresh token beside it, the login refreshes and stores the
// rotated pair rather than failing on a token that was good a minute ago.
func TestTokenLoginRefreshesAnExpiredAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["refresh"] != "pasted-refresh" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"auth_token":"fresh","refresh":"fresh-refresh"}`)
		case "/api/v1/users/me":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"detail":"Token is expired"}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	app, _, stderr, credentials := testApp(t, server)
	app.In = strings.NewReader(`{"auth_token":"expired","refresh":"pasted-refresh"}` + "\n")
	if code := app.Execute(context.Background(), []string{"--profile", "pasted", "--api-url", server.URL + "/api/v1/", "--json", "auth", "login", "--with-token"}); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	saved := credentials.values[credential.Account("pasted", server.URL+"/api/v1/")]
	if saved.AuthToken != "fresh" || saved.RefreshToken != "fresh-refresh" {
		t.Fatalf("saved %+v, want the rotated pair", saved)
	}
}

func TestConfirmTakesEnterAsYes(t *testing.T) {
	for input, want := range map[string]bool{"\n": true, "y\n": true, "Yes\n": true, "n\n": false, "no\n": false, "maybe\n": false} {
		app := &App{In: strings.NewReader(input), Err: &bytes.Buffer{}}
		got, err := app.confirm("Proceed?")
		if err != nil || got != want {
			t.Errorf("input %q: got %t, %v; want %t", input, got, err, want)
		}
	}
}

func TestReadLineOrFallsBackOnEnter(t *testing.T) {
	for input, want := range map[string]string{"\n": "https://tree.taiga.io/", "  \n": "https://tree.taiga.io/", "https://taiga.example/\n": "https://taiga.example/", "https://taiga.example/": "https://taiga.example/"} {
		prompts := &bytes.Buffer{}
		app := &App{In: strings.NewReader(input), Err: prompts}
		got, err := app.readLineOr("Taiga URL", "https://tree.taiga.io/")
		if err != nil || got != want {
			t.Errorf("input %q: got %q, %v; want %q", input, got, err, want)
		}
		if prompts.String() != "Taiga URL [https://tree.taiga.io/]: " {
			t.Errorf("prompt = %q", prompts.String())
		}
	}
}

func TestReadChoiceTakesANumberOrTheFirstOnEnter(t *testing.T) {
	for input, want := range map[string]int{"\n": 0, "2\n": 1, "1": 0} {
		app := &App{In: strings.NewReader(input), Err: &bytes.Buffer{}}
		got, err := app.readChoice("Pick", []string{"first", "second"})
		if err != nil || got != want {
			t.Errorf("input %q: got %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"3\n", "0\n", "x\n"} {
		app := &App{In: strings.NewReader(input), Err: &bytes.Buffer{}}
		if _, err := app.readChoice("Pick", []string{"first", "second"}); err == nil {
			t.Errorf("input %q was accepted", input)
		}
	}
}

// terminalApp is an app whose input answers as a terminal would, so that the
// questions a login asks can be tested against a buffer.
func terminalApp(t *testing.T, server *httptest.Server, input string) (*App, *bytes.Buffer) {
	t.Helper()
	app, _, stderr, _ := testApp(t, server)
	app.In = strings.NewReader(input)
	app.StdinTTY = func() bool { return true }
	return app, stderr
}

// A profile that has logged in before saved a URL, but the login is also the
// moment someone points the CLI at another Taiga, so the saved URL is offered
// as the default rather than used without a word. Enter keeps it, and keeping
// it contacts nothing: it is already the API's address.
func TestLoginOffersTheSavedURLAndKeepsItOnEnter(t *testing.T) {
	const saved = "https://saved.invalid/api/v1/"
	app, prompts := terminalApp(t, nil, "\n")
	target, err := app.loginTarget(context.Background(), "", Settings{APIURL: saved})
	if err != nil {
		t.Fatalf("loginTarget: %v (prompts=%q)", err, prompts.String())
	}
	if target.apiURL != saved {
		t.Errorf("apiURL = %q, want the saved %q", target.apiURL, saved)
	}
	if !strings.Contains(prompts.String(), "["+saved+"]") {
		t.Errorf("prompt = %q, want the saved URL offered as the default", prompts.String())
	}
}

// Answering with another URL moves the login to that Taiga, which is the whole
// point of asking.
func TestLoginDiscoversTheURLTypedOverTheSavedOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/conf.json":
			_, _ = io.WriteString(w, `{"api":"http://`+r.Host+`/api/v1/","baseHref":"/"}`)
		case "/api/v1/locales":
			_, _ = io.WriteString(w, `[{"code":"en","name":"English"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	app, prompts := terminalApp(t, server, server.URL+"/\n")
	target, err := app.loginTarget(context.Background(), "", Settings{APIURL: "https://saved.invalid/api/v1/"})
	if err != nil {
		t.Fatalf("loginTarget: %v (prompts=%q)", err, prompts.String())
	}
	if target.apiURL != server.URL+"/api/v1/" {
		t.Errorf("apiURL = %q, want the API behind the typed URL", target.apiURL)
	}
}

// A URL this invocation named itself is a decision rather than a default, so
// --api-url and the environment are obeyed without a question.
func TestLoginDoesNotAskWhenTheAPIURLWasGiven(t *testing.T) {
	const given = "https://given.invalid/api/v1/"
	for name, prepare := range map[string]func(*App){
		"--api-url": func(app *App) { app.global.APIURL = given },
		"TAIGA_API_URL": func(app *App) {
			app.Getenv = func(name string) string { return map[string]string{"TAIGA_API_URL": given}[name] }
		},
	} {
		t.Run(name, func(t *testing.T) {
			app, prompts := terminalApp(t, nil, "https://typed.invalid/\n")
			prepare(app)
			target, err := app.loginTarget(context.Background(), "", Settings{APIURL: given})
			if err != nil {
				t.Fatalf("loginTarget: %v", err)
			}
			if target.apiURL != given {
				t.Errorf("apiURL = %q, want the given %q", target.apiURL, given)
			}
			if prompts.String() != "" {
				t.Errorf("prompt = %q, want nothing asked", prompts.String())
			}
		})
	}
}

// A script has nobody to ask, so the URL its profile saved is kept as it was
// before there was a question at all.
func TestLoginKeepsTheSavedURLOffATerminal(t *testing.T) {
	const saved = "https://saved.invalid/api/v1/"
	app, _, prompts, _ := testApp(t, nil)
	target, err := app.loginTarget(context.Background(), "", Settings{APIURL: saved})
	if err != nil {
		t.Fatalf("loginTarget: %v", err)
	}
	if target.apiURL != saved {
		t.Errorf("apiURL = %q, want the saved %q", target.apiURL, saved)
	}
	if prompts.String() != "" {
		t.Errorf("prompt = %q, want nothing asked", prompts.String())
	}
}

// A login that lands without a refresh token dies when the access token
// expires, and saying so without saying what to do about it leaves the person
// exactly where the message found them. The way out is the same console
// one-liner the wizard already offers, so the notice carries it.
func TestTokenLoginSaysHowToGetARefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/users/me" {
			_, _ = io.WriteString(w, `{"id":1,"username":"demo"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	for name, tc := range map[string]struct {
		stdin      string
		wantAdvice bool
	}{
		"bare token":     {stdin: "pasted-token", wantAdvice: true},
		"with a refresh": {stdin: `{"auth_token":"pasted-token","refresh":"pasted-refresh"}`, wantAdvice: false},
	} {
		t.Run(name, func(t *testing.T) {
			app, out, stderr, _ := testApp(t, server)
			app.In = strings.NewReader(tc.stdin + "\n")
			if code := app.Execute(context.Background(), []string{"--profile", "advice", "--api-url", server.URL + "/api/v1/", "auth", "login", "--with-token"}); code != ExitSuccess {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			hasAdvice := strings.Contains(out.String(), consoleCopySnippet)
			if hasAdvice != tc.wantAdvice {
				t.Errorf("advice shown = %t, want %t; stdout = %q", hasAdvice, tc.wantAdvice, out.String())
			}
		})
	}
}
