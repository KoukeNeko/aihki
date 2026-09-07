package taiga

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// taigaSite serves what a Taiga web app and its API serve, mounted under
// prefix: conf.json naming the API, the API's locale list, and the app's page
// for every other path, which is how its server answers a deep link.
type taigaSite struct {
	*httptest.Server
	prefix string
	mu     sync.Mutex
	paths  []string
}

func newTaigaSite(t *testing.T, prefix string) *taigaSite {
	t.Helper()
	site := &taigaSite{prefix: prefix}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.paths = append(site.paths, r.URL.Path)
		site.mu.Unlock()
		switch r.URL.Path {
		case prefix + "/conf.json":
			_, _ = io.WriteString(w, `{"api":"`+site.URL+prefix+`/api/v1/","baseHref":"`+prefix+`/"}`)
		case prefix + "/api/v1/locales":
			_, _ = io.WriteString(w, `[{"code":"en","name":"English","bidi":false}]`)
		default:
			_, _ = io.WriteString(w, `<!doctype html><html><body>taiga</body></html>`)
		}
	}))
	t.Cleanup(site.Close)
	return site
}

func (site *taigaSite) requested() []string {
	site.mu.Lock()
	defer site.mu.Unlock()
	return append([]string(nil), site.paths...)
}

// The address people have is the page in front of them, and a page sits
// somewhere below the app, which may itself sit below the site's root. Each
// path above the page is tried until conf.json turns up, and the API it
// names has to answer as Taiga before it counts.
func TestDiscoverAPIFindsTheAppFromAnyPageInsideIt(t *testing.T) {
	for _, prefix := range []string{"", "/taiga"} {
		t.Run("app at "+prefix+"/", func(t *testing.T) {
			site := newTaigaSite(t, prefix)
			config, err := DiscoverAPI(context.Background(), site.Client(), site.URL+prefix+"/project/demo/backlog")
			if err != nil {
				t.Fatalf("DiscoverAPI: %v", err)
			}
			if config.API != site.URL+prefix+"/api/v1/" {
				t.Errorf("API = %q", config.API)
			}
			if config.Site != site.URL+prefix+"/" {
				t.Errorf("Site = %q, want the app's address", config.Site)
			}
			want := []string{prefix + "/project/demo/backlog/conf.json", prefix + "/project/demo/conf.json", prefix + "/project/conf.json", prefix + "/conf.json", prefix + "/api/v1/locales"}
			if got := site.requested(); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("requested %v, want %v", got, want)
			}
		})
	}
}

// People pass the API's address as readily as the web app's, and the API
// answers its locale list to anyone, so the address itself and its api/v1/
// path are tried before giving up.
func TestDiscoverAPIAcceptsTheAPIAddress(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/v1/locales" {
			_, _ = io.WriteString(w, `[{"code":"en","name":"English","bidi":false}]`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	for _, address := range []string{server.URL + "/", server.URL + "/api/v1/"} {
		paths = nil
		config, err := DiscoverAPI(context.Background(), server.Client(), address)
		if err != nil {
			t.Fatalf("address %s: %v", address, err)
		}
		if config.API != server.URL+"/api/v1/" || config.Site != "" {
			t.Errorf("address %s: config = %+v", address, config)
		}
		for _, path := range paths {
			if strings.Contains(path, "/api/v1/api/") {
				t.Errorf("address %s probed %s, which stacks api/v1 on itself", address, path)
			}
		}
	}
}

// A host that is not a Taiga web app -- a forum, a marketing site -- answers
// 404 or a page of HTML. The person who typed it needs to see every URL that
// was tried and what came back, not a bare status, and the kind still follows
// what the typed address's conf.json answered.
func TestDiscoverAPINamesEveryURLTriedWhenAHostIsNotTaiga(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		answer func(http.ResponseWriter)
		kind   ErrorKind
		reason string
	}{
		{"forum answering 404", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"errors":["The requested URL or resource could not be found."],"error_type":"not_found"}`)
		}, KindNotFound, "conf.json answered 404 Not Found"},
		{"site answering a page", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `<!doctype html><html><body>welcome</body></html>`)
		}, KindValidation, "conf.json answered with something other than JSON"},
		{"configuration without an API", func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"baseHref":"/"}`)
		}, KindValidation, "conf.json answered JSON that names no API URL"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				testCase.answer(w)
			}))
			defer server.Close()

			_, err := DiscoverAPI(context.Background(), server.Client(), server.URL+"/")
			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("got %v, want an *Error", err)
			}
			if apiErr.Kind != testCase.kind {
				t.Errorf("kind = %s, want %s", apiErr.Kind, testCase.kind)
			}
			if want := []string{"/conf.json", "/locales", "/api/v1/locales"}; strings.Join(paths, " ") != strings.Join(want, " ") {
				t.Errorf("probed %v, want %v", paths, want)
			}
			for _, expected := range []string{"sending no credentials", server.URL + "/" + testCase.reason, server.URL + "/api/v1/locales answered", "any page inside the Taiga web app", HostedTaigaApp} {
				if !strings.Contains(apiErr.Message, expected) {
					t.Errorf("message = %q, want it to contain %q", apiErr.Message, expected)
				}
			}
		})
	}
}

// A web app's server answers any unknown path with its page, so the page at
// /locales must not be mistaken for the API's locale list.
func TestDiscoverAPIDoesNotMistakeAPageForTheAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/locales" {
			_, _ = io.WriteString(w, `<!doctype html><html><body>app</body></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := DiscoverAPI(context.Background(), server.Client(), server.URL+"/")
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v, want an *Error", err)
	}
	if !strings.Contains(apiErr.Message, server.URL+"/locales answered with something other than JSON") {
		t.Errorf("message = %q, want the page at /locales named", apiErr.Message)
	}
}

// conf.json is not proof of Taiga on its own; the API it names has to answer
// as Taiga, or a credential would go to whatever the file happened to name.
func TestDiscoverAPIRejectsAConfigurationWhoseAPIDoesNotAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conf.json" {
			_, _ = io.WriteString(w, `{"api":"`+"http://"+r.Host+`/nowhere/"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := DiscoverAPI(context.Background(), server.Client(), server.URL+"/")
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v, want an *Error", err)
	}
	if !strings.Contains(apiErr.Message, "names an API at "+server.URL+"/nowhere/ that answered 404 Not Found") {
		t.Errorf("message = %q, want the unanswering API named", apiErr.Message)
	}
}

// The typed site is the only site discovery may contact. A redirect within
// it is followed; a redirect to another site is reported, not followed, so
// that a Location header cannot widen where discovery goes.
func TestDiscoverAPIFollowsRedirectsWithinTheSiteOnly(t *testing.T) {
	t.Run("within the site", func(t *testing.T) {
		// The site answers its root by redirecting to where the app lives.
		redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/conf.json":
				http.Redirect(w, r, "/taiga/conf.json", http.StatusFound)
			case "/taiga/conf.json":
				_, _ = io.WriteString(w, `{"api":"`+"http://"+r.Host+`/taiga/api/v1/","baseHref":"/taiga/"}`)
			case "/taiga/api/v1/locales":
				_, _ = io.WriteString(w, `[{"code":"en"}]`)
			default:
				http.NotFound(w, r)
			}
		}))
		defer redirecting.Close()
		config, err := DiscoverAPI(context.Background(), redirecting.Client(), redirecting.URL+"/")
		if err != nil {
			t.Fatalf("DiscoverAPI: %v", err)
		}
		if config.API != redirecting.URL+"/taiga/api/v1/" {
			t.Errorf("API = %q", config.API)
		}
	})
	t.Run("to another site", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://other.example"+r.URL.Path, http.StatusFound)
		}))
		defer server.Close()
		_, err := DiscoverAPI(context.Background(), server.Client(), server.URL+"/")
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("got %v, want an *Error", err)
		}
		if !strings.Contains(apiErr.Message, server.URL+"/conf.json redirects to https://other.example/conf.json, a different site") {
			t.Errorf("message = %q, want the redirect reported", apiErr.Message)
		}
	})
}

// A site that cannot be reached is a network problem, not a wrong address,
// and is reported as one: the URL, the cause, and that nothing was sent.
func TestDiscoverAPIReportsAnUnreachableSite(t *testing.T) {
	_, err := DiscoverAPI(context.Background(), &http.Client{Transport: failingTransport{}}, "https://taiga.example/")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindTransport || !apiErr.Retryable {
		t.Fatalf("got %v, want a retryable %s", err, KindTransport)
	}
	for _, expected := range []string{"could not reach https://taiga.example/conf.json", "connection lost", "no credentials were sent"} {
		if !strings.Contains(apiErr.Message, expected) {
			t.Errorf("message = %q, want it to contain %q", apiErr.Message, expected)
		}
	}
}

// Under the hosted Taiga's domain the web app has one address, so a forum or
// a marketing host there is told where the app is rather than given an example.
func TestHostedTaigaForKnowsItsOwnDomainOnly(t *testing.T) {
	for site, want := range map[string]bool{
		"https://community.taiga.io/":  true,
		"https://taiga.io/pricing":     true,
		"https://api.taiga.io/api/v1/": true,
		"https://tree.taiga.io/":       false,
		"https://taiga.example.com/":   false,
		"https://nottaiga.io/":         false,
	} {
		app, ok := HostedTaigaFor(site)
		if ok != want || (ok && app != HostedTaigaApp) {
			t.Errorf("%s: got %q, %t; want %t", site, app, ok, want)
		}
	}
	if !strings.Contains(discoveryAdvice("https://community.taiga.io/"), "The hosted Taiga web app is "+HostedTaigaApp) {
		t.Error("advice for a taiga.io host must name the hosted app")
	}
	if !strings.Contains(discoveryAdvice("https://taiga.example.com/"), "such as a project or backlog page") {
		t.Error("advice for another site must describe what to paste")
	}
}
