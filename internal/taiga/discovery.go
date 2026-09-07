package taiga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// FrontConfig is what a Taiga web app's conf.json declares, plus where it was
// found. Site is the address of the web app, empty when only the API was
// found.
type FrontConfig struct {
	API       string `json:"api"`
	EventsURL string `json:"eventsUrl"`
	BaseHref  string `json:"baseHref"`
	Site      string `json:"-"`
}

const (
	// hostedTaigaDomain is the domain of the Taiga its makers run. Its web app
	// lives at one address, and every other name under the domain is a forum,
	// a site or the API, so a host there that is not the app can be told where
	// the app is as a fact rather than as an example.
	hostedTaigaDomain = "taiga.io"
	// HostedTaigaApp is the address of the hosted Taiga web app.
	HostedTaigaApp = "https://tree.taiga.io/"
	// maxDiscoveryBytes bounds what a probe reads: a frontend configuration
	// and a locale list are both a few kilobytes.
	maxDiscoveryBytes = 1 << 20
	maxRedirects      = 10
)

// discoveryAttempt is one URL discovery tried and what it found there.
type discoveryAttempt struct {
	url     *url.URL
	outcome string
}

// discoveryProbe is what one fetch came back with. RedirectedTo is set when
// the answer was a redirect to another site, which discovery does not follow.
type discoveryProbe struct {
	status       int
	body         []byte
	redirectedTo *url.URL
}

// crossSiteRedirect is the error the discovery client's redirect policy
// returns, so that a redirect to another site is reported rather than
// followed: the address the person typed is the only site discovery may
// contact, and a Location header must not widen that.
type crossSiteRedirect struct {
	to *url.URL
}

func (r *crossSiteRedirect) Error() string { return "redirects to " + r.to.String() }

// DiscoverAPI finds the Taiga API behind site, which is the URL of any page
// inside a Taiga web app, or the API's own address. The web app's conf.json
// names the API, so it is looked for at the page's path and at each path
// above it; failing that, the address itself and its api/v1/ path are tried
// as the API. A configuration counts only when the API it names answers as
// Taiga. Nothing beyond the site that was typed is contacted, and nothing is
// sent with a credential.
func DiscoverAPI(ctx context.Context, httpClient *http.Client, site string) (FrontConfig, error) {
	typed, err := parseSiteURL(site)
	if err != nil {
		return FrontConfig{}, err
	}
	// One budget for every probe, the way a JSON request is bounded.
	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	client := discoveryClient(httpClient)
	var tried []discoveryAttempt
	var first *discoveryProbe
	for _, base := range siteCandidates(typed) {
		confURL := base.ResolveReference(&url.URL{Path: "conf.json"})
		probe, err := fetchDiscovery(ctx, client, confURL)
		if err != nil {
			if first == nil {
				return FrontConfig{}, unreachable(confURL, err)
			}
			tried = append(tried, discoveryAttempt{url: confURL, outcome: "could not be reached: " + describeCause(err)})
			continue
		}
		if first == nil {
			first = probe
		}
		config, outcome := parseFrontConfig(probe)
		if outcome == "" {
			found, apiOutcome := probeAPI(ctx, client, localesURL(config.API))
			if found {
				config.Site = base.String()
				return config, nil
			}
			outcome = fmt.Sprintf("names an API at %s that %s", config.API, apiOutcome)
		}
		tried = append(tried, discoveryAttempt{url: confURL, outcome: outcome})
	}
	for _, base := range apiCandidates(typed) {
		found, outcome := probeAPI(ctx, client, localesURL(base.String()))
		if found {
			api, err := NormalizeAPIURL(base.String())
			if err != nil {
				return FrontConfig{}, err
			}
			return FrontConfig{API: api}, nil
		}
		tried = append(tried, discoveryAttempt{url: localesURL(base.String()), outcome: outcome})
	}
	return FrontConfig{}, notTaigaError(typed, first, tried)
}

func parseSiteURL(site string) (*url.URL, error) {
	site = strings.TrimSpace(site)
	parsed, err := url.Parse(site)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Taiga URL %q", site)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported Taiga URL scheme %q", parsed.Scheme)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed, nil
}

// siteCandidates lists where the web app may be, from the typed path up to
// the site's root: a page inside the app sits below the app, and the app may
// sit below the root.
func siteCandidates(typed *url.URL) []*url.URL {
	var candidates []*url.URL
	current := typed.Path
	for {
		candidate := *typed
		candidate.Path = current
		candidates = append(candidates, &candidate)
		if current == "/" {
			return candidates
		}
		current = path.Dir(strings.TrimSuffix(current, "/"))
		if current != "/" {
			current += "/"
		}
	}
}

// apiCandidates lists where the API may be when the address is the API's
// own: at the address itself, and under api/v1/ unless it is that already.
func apiCandidates(typed *url.URL) []*url.URL {
	candidates := []*url.URL{typed}
	if !strings.HasSuffix(typed.Path, "/api/v1/") {
		candidates = append(candidates, typed.ResolveReference(&url.URL{Path: "api/v1/"}))
	}
	return candidates
}

func localesURL(apiBase string) *url.URL {
	base, err := url.Parse(apiBase)
	if err != nil {
		return &url.URL{}
	}
	return base.ResolveReference(&url.URL{Path: "locales"})
}

// discoveryClient is the caller's client with a redirect policy: a redirect
// within the site is followed, one to another site or from HTTPS down to HTTP
// is reported instead.
func discoveryClient(httpClient *http.Client) *http.Client {
	client := *httpClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		origin := via[0].URL
		if req.URL.Host != origin.Host || (origin.Scheme == "https" && req.URL.Scheme != "https") {
			return &crossSiteRedirect{to: req.URL}
		}
		return nil
	}
	return &client
}

func fetchDiscovery(ctx context.Context, client *http.Client, target *url.URL) (*discoveryProbe, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		var redirect *crossSiteRedirect
		if errors.As(err, &redirect) {
			return &discoveryProbe{redirectedTo: redirect.to}, nil
		}
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryBytes))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read discovery response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close discovery response: %w", closeErr)
	}
	return &discoveryProbe{status: resp.StatusCode, body: data}, nil
}

// parseFrontConfig reads a conf.json answer. The outcome is empty when the
// answer is a usable configuration, and otherwise says what came back instead.
func parseFrontConfig(probe *discoveryProbe) (FrontConfig, string) {
	if probe.redirectedTo != nil {
		return FrontConfig{}, redirected(probe.redirectedTo)
	}
	if probe.status != http.StatusOK {
		return FrontConfig{}, answered(probe.status)
	}
	var config FrontConfig
	if err := json.Unmarshal(probe.body, &config); err != nil {
		return FrontConfig{}, "answered with something other than JSON"
	}
	if config.API == "" {
		return FrontConfig{}, "answered JSON that names no API URL"
	}
	api, err := NormalizeAPIURL(config.API)
	if err != nil {
		return FrontConfig{}, "names an API URL that cannot be used: " + err.Error()
	}
	config.API = api
	return config, ""
}

// probeAPI reports whether target is Taiga's locale list, which every Taiga
// API serves without a credential. A web app's server answers unknown paths
// with its page, so only a JSON list of locales counts.
func probeAPI(ctx context.Context, client *http.Client, target *url.URL) (bool, string) {
	probe, err := fetchDiscovery(ctx, client, target)
	if err != nil {
		return false, "could not be reached: " + describeCause(err)
	}
	if probe.redirectedTo != nil {
		return false, redirected(probe.redirectedTo)
	}
	if probe.status != http.StatusOK {
		return false, answered(probe.status)
	}
	var locales []struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(probe.body, &locales); err != nil {
		return false, "answered with something other than JSON"
	}
	if len(locales) == 0 || locales[0].Code == "" {
		return false, "answered JSON that is not a Taiga locale list"
	}
	return true, ""
}

func answered(status int) string {
	return fmt.Sprintf("answered %d %s", status, http.StatusText(status))
}

func redirected(to *url.URL) string {
	return fmt.Sprintf("redirects to %s, a different site; pass that address if it is your Taiga", to)
}

// describeCause strips the request framing Go's client wraps around a
// network failure, leaving the part a person can act on.
func describeCause(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}
	return err.Error()
}

// unreachable reports a site that could not be contacted at all. That is a
// network problem, not a wrong address, and is not dressed up as one.
func unreachable(confURL *url.URL, err error) *Error {
	return &Error{Kind: KindTransport, Operation: "GET " + confURL.Path, Message: fmt.Sprintf("could not reach %s: %s; no credentials were sent", confURL, describeCause(err)), Retryable: true, Cause: err}
}

// notTaigaError reports a site that is neither a Taiga web app nor its API.
// The kind follows what the typed address's conf.json answered, so that a 404
// stays not_found and a page where JSON was expected is validation; the
// message lists every URL tried, because the person who typed the address
// needs to see where it led.
func notTaigaError(typed *url.URL, first *discoveryProbe, tried []discoveryAttempt) *Error {
	attempts := make([]string, 0, len(tried))
	for _, attempt := range tried {
		attempts = append(attempts, attempt.url.String()+" "+attempt.outcome)
	}
	message := fmt.Sprintf("%s is not the address of a Taiga web app or API. Tried, sending no credentials: %s. %s", typed, strings.Join(attempts, "; "), discoveryAdvice(typed.String()))
	operation := "GET " + typed.ResolveReference(&url.URL{Path: "conf.json"}).Path
	if first != nil && first.redirectedTo == nil && first.status != http.StatusOK {
		apiErr := decodeAPIError(operation, first.status, first.body)
		apiErr.Message = message
		return apiErr
	}
	return &Error{Kind: KindValidation, Operation: operation, Message: message, Retryable: false}
}

// HostedTaigaFor names the hosted web app for a site under the hosted Taiga's
// domain that is not the app itself, such as the forum, the marketing site or
// the API, and reports false for any other site. It is a fact about one domain
// rather than a guess, which is what lets a login offer it.
func HostedTaigaFor(site string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(site))
	if err != nil {
		return "", false
	}
	hostname := parsed.Hostname()
	hosted := hostname == hostedTaigaDomain || strings.HasSuffix(hostname, "."+hostedTaigaDomain)
	if !hosted || "https://"+hostname+"/" == HostedTaigaApp {
		return "", false
	}
	return HostedTaigaApp, true
}

// discoveryAdvice says what the address should have been. Under the hosted
// Taiga's domain the web app has one known address, which is stated;
// elsewhere it can only be described.
func discoveryAdvice(site string) string {
	if app, ok := HostedTaigaFor(site); ok {
		return "The hosted Taiga web app is " + app + "; paste the URL of any page inside it"
	}
	return "Paste the URL of any page inside the Taiga web app, such as a project or backlog page; the hosted Taiga web app is " + HostedTaigaApp
}
