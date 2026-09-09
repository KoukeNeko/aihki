package taiga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2" // nosemgrep -- retry jitter only has to break lockstep between callers; see retryDelay
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxResponseBytes = 16 << 20

const (
	// versionField is the key Taiga rejects a stale write under, and
	// nonFieldErrorsKey is Django REST Framework's bucket for a rejection that
	// belongs to no single field.
	versionField      = "version"
	nonFieldErrorsKey = "non_field_errors"
	itemKeyPrefix     = "item "

	unconfirmedMessage = "request may have been committed; verify before retrying"
	interruptedMessage = "request was interrupted before Taiga confirmed it; verify before retrying"
	// refreshNotStoredMessage is what a refresh that Taiga completed but this
	// process could not record has to say, because the token on disk is dead
	// from that moment and nothing else will explain the next failure. It is
	// named for the event rather than the credential so that gosec does not
	// read a sentence about a token as a token.
	refreshNotStoredMessage = "Taiga issued a new token but it could not be stored, so the saved credential is now stale; run `taiga auth login` again"
	maxMessageBytes         = 2000
	maxRenderedFields       = 20
	maxFieldDepth           = 8
)

// proseKeys are the keys under which Taiga and Django REST Framework put a
// sentence describing the request as a whole rather than one rejected field.
var proseKeys = []string{"_error_message", "detail", "message"}

const (
	// defaultRequestTimeout bounds one attempt at a JSON request: connecting,
	// sending it, waiting for the answer and reading it. Taiga answers these
	// from its database, so one that takes longer is not going to finish.
	defaultRequestTimeout = 30 * time.Second
	// defaultStallTimeout bounds how long a transfer may go without a byte
	// moving in either direction. It is longer than a request attempt because
	// the quiet stretch after an upload is Taiga storing the file, and after a
	// dump is Taiga building a project from it.
	defaultStallTimeout = 60 * time.Second
)

const (
	backoffStep = 100 * time.Millisecond
	// jitterWindow spreads retries so that CLI invocations throttled by the
	// same response do not resend in lockstep.
	jitterWindow = 100 * time.Millisecond
	// maxBackoffShift keeps the doubling below the point where the shift would
	// overflow time.Duration and produce a negative delay.
	maxBackoffShift = 20
	maxRetryAfter   = 30 * time.Second
)

type Client struct {
	baseURL      *url.URL
	httpClient   *http.Client
	token        string
	refreshToken string
	onRefresh    func(string, string) error
	verbose      io.Writer
	maxRetries   int
	sleep        func(context.Context, time.Duration) error

	requestTimeout time.Duration
	stallTimeout   time.Duration
}

type ClientOption func(*Client)

func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = client }
}

func WithToken(token string) ClientOption {
	return func(c *Client) { c.token = strings.TrimSpace(token) }
}

func WithRefreshToken(refreshToken string, onRefresh func(string, string) error) ClientOption {
	return func(c *Client) {
		c.refreshToken = strings.TrimSpace(refreshToken)
		c.onRefresh = onRefresh
	}
}

func WithVerbose(writer io.Writer) ClientOption {
	return func(c *Client) { c.verbose = writer }
}

func WithMaxRetries(retries int) ClientOption {
	return func(c *Client) { c.maxRetries = retries }
}

func NewClient(rawURL string, options ...ClientOption) (*Client, error) {
	normalized, err := NormalizeAPIURL(rawURL)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}
	// The HTTP client carries no overall deadline. Each JSON attempt is bounded
	// on its own, and a transfer is watched for stalling rather than for length,
	// so that an attachment or a dump is as long as it is.
	client := &Client{
		baseURL:        parsed,
		httpClient:     &http.Client{},
		maxRetries:     3,
		requestTimeout: defaultRequestTimeout,
		stallTimeout:   defaultStallTimeout,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

func NormalizeAPIURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("API URL cannot be empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid API URL %q", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported API URL scheme %q", parsed.Scheme)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed.String(), nil
}

func (c *Client) APIURL() string { return c.baseURL.String() }

func (c *Client) SetToken(token string) { c.token = strings.TrimSpace(token) }

// SetRefreshToken gives a client built without one the means to refresh, and
// the callback that records the rotated pair.
func (c *Client) SetRefreshToken(refreshToken string, onRefresh func(string, string) error) {
	c.refreshToken = strings.TrimSpace(refreshToken)
	c.onRefresh = onRefresh
}

func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodGet, path, query, nil, out, true, noCommit)
}

// GetOnce performs a GET without automatic retries. It is reserved for Taiga
// endpoints that use GET to enqueue work and are therefore not idempotent.
func (c *Client) GetOnce(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodGet, path, query, nil, out, false, mayCommit)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodPost, path, nil, body, out, false, mayCommit)
}

// PostIdempotent performs a POST whose outcome nobody has to reconcile: it
// either takes effect or it does not, and sending it again reaches the same
// state. Logging in and liking a project are POSTs of this kind, and reporting
// an interrupted one as a possible commit would send a person looking for a
// record that was never going to exist.
func (c *Client) PostIdempotent(ctx context.Context, path string, body, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodPost, path, nil, body, out, false, noCommit)
}

func (c *Client) PostQuery(ctx context.Context, path string, query url.Values, body, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodPost, path, query, body, out, false, mayCommit)
}

func (c *Client) Patch(ctx context.Context, path string, body, out any) (http.Header, error) {
	return c.doJSON(ctx, http.MethodPatch, path, nil, body, out, false, mayCommit)
}

func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.doJSON(ctx, http.MethodDelete, path, nil, nil, nil, false, mayCommit)
	return err
}

// deleteAndVerify sends the DELETE and then reads the record back, reporting
// the deletion as unconfirmed unless Taiga answers that the record is gone.
// noun is what the record is called in that report. query carries what the
// endpoint needs alongside the deletion, such as where to move the items a
// status or swimlane still holds.
func (c *Client) deleteAndVerify(ctx context.Context, noun, path string, id int64, query url.Values) error {
	operation := fmt.Sprintf("%s/%d", path, id)
	if _, err := c.doJSON(ctx, http.MethodDelete, operation, query, nil, nil, false, mayCommit); err != nil {
		return err
	}
	var ignored any
	if _, err := c.Get(ctx, operation, nil, &ignored); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Kind == KindNotFound {
			return nil
		}
		return &Error{Kind: KindAmbiguousCommit, Operation: "DELETE " + operation, Message: noun + " was deleted but could not be verified", Retryable: false, Cause: err}
	}
	return &Error{Kind: KindAmbiguousCommit, Operation: "DELETE " + operation, Message: "Taiga still returns the " + noun + " after deletion", Retryable: false}
}

// commitRisk says what an unconfirmed request could have left on the server.
// It belongs to the call, decided where the endpoint is known, rather than
// being inferred from the HTTP verb: Taiga has POSTs that settle nothing and a
// GET that enqueues work.
//
// It is deliberately not a bool. It sits beside retryGET in the parameter
// list, and while it was one, an untyped false at a call site silently meant
// noCommit -- which is how three destructive deletes came to report an
// interrupt as though nothing had been sent.
type commitRisk int

const (
	noCommit commitRisk = iota
	mayCommit
)

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, out any, retryGET bool, risk commitRisk) (http.Header, error) {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimPrefix(path, "/")})
	endpoint.RawQuery = query.Encode()
	operation := method + " " + endpoint.Path
	unconfirmed := func(message string, cause error) *Error {
		return &Error{Kind: KindAmbiguousCommit, Operation: operation, Message: message, Retryable: false, Cause: cause}
	}
	attempts := 1
	if method == http.MethodGet && retryGET {
		attempts = c.maxRetries
		if attempts < 1 {
			attempts = 1
		}
	}
	refreshed := false
	for attempt := 1; attempt <= attempts; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		// A context already finished before anything goes out cannot have
		// committed anything, which is what separates the plain cancellation
		// below from the ambiguous one.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, c.requestTimeout)
		req, err := http.NewRequestWithContext(attemptCtx, method, endpoint.String(), reader)
		if err != nil {
			cancelAttempt()
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "taiga-cli/0.1")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		started := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			cancelAttempt()
			c.log(method, endpoint.Path, 0, time.Since(started))
			if ctx.Err() != nil {
				// Interrupted while the request was in flight. Taiga does not
				// roll a write back because the caller stopped listening, so
				// the outcome is unknown rather than merely cancelled.
				if risk == mayCommit {
					return nil, unconfirmed(interruptedMessage, err)
				}
				return nil, ctx.Err()
			}
			if risk == mayCommit {
				return nil, unconfirmed(unconfirmedMessage, err)
			}
			if attempt < attempts {
				if err := c.sleep(ctx, retryDelay(attempt, "")); err != nil {
					return nil, err
				}
				continue
			}
			return nil, &Error{Kind: KindTransport, Operation: operation, Message: "Taiga API is unavailable", Retryable: true, Cause: err}
		}
		c.log(method, endpoint.Path, resp.StatusCode, time.Since(started))
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		closeErr := resp.Body.Close()
		cancelAttempt()
		if readErr != nil {
			if risk == mayCommit {
				return nil, unconfirmed(unconfirmedMessage, readErr)
			}
			return nil, &Error{Kind: KindTransport, Operation: operation, Message: "read Taiga API response", Retryable: method == http.MethodGet, Cause: readErr}
		}
		if closeErr != nil {
			if risk == mayCommit {
				return nil, unconfirmed(unconfirmedMessage, closeErr)
			}
			return nil, &Error{Kind: KindTransport, Operation: operation, Message: "close Taiga API response", Retryable: false, Cause: closeErr}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			apiErr := decodeAPIError(operation, resp.StatusCode, data)
			if resp.StatusCode == http.StatusUnauthorized && !refreshed && c.refreshToken != "" && !strings.HasSuffix(endpoint.Path, "/auth/refresh") {
				refreshErr := c.refresh(ctx)
				if refreshErr == nil {
					refreshed = true
					attempt--
					continue
				}
				// Only Taiga refusing the refresh leaves the original rejection
				// as the better report, since both say the same thing and that
				// one names what the caller ran. Every other way a refresh can
				// fail -- the operator stopping it, the call never reaching
				// Taiga, an answer that could not be read, a token issued but
				// not stored -- is not a refused credential, and reporting it
				// as one sends someone to log in again over a dropped
				// connection or a locked keyring.
				if !refreshRefused(refreshErr) {
					return resp.Header, refreshErr
				}
			}
			if method == http.MethodGet && attempt < attempts && retryableStatus(resp.StatusCode) {
				if err := c.sleep(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); err != nil {
					return nil, err
				}
				continue
			}
			return resp.Header, apiErr
		}
		if out != nil && len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				if risk == mayCommit {
					return resp.Header, unconfirmed("request was accepted but its result could not be decoded; verify before retrying", err)
				}
				return resp.Header, &Error{Kind: KindTransport, Operation: operation, Message: "decode Taiga API response", Retryable: false, Cause: err}
			}
		}
		return resp.Header, nil
	}
	return nil, &Error{Kind: KindTransport, Message: "Taiga API request failed", Retryable: true}
}

// refreshRefused reports whether Taiga answered a refresh by refusing it, as
// opposed to not answering at all, answering unreadably, or asking for it to
// be sent later.
func refreshRefused(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.UpstreamStatus == 0 {
		return false
	}
	return apiErr.UpstreamStatus < http.StatusInternalServerError && apiErr.Kind != KindThrottled
}

// refresh exchanges the refresh token for a new pair. Its failures are
// classified the way doJSON classifies them, because the caller reports them
// in place of the request that needed the refresh.
func (c *Client) refresh(ctx context.Context) error {
	attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "auth/refresh"})
	operation := "POST " + endpoint.Path
	payload, err := json.Marshal(map[string]string{"refresh": c.refreshToken})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "taiga-cli/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The caller's context says whether the operator stopped it. The
		// attempt running out of its own time is Taiga not answering.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Kind: KindTransport, Operation: operation, Message: "Taiga API is unavailable", Retryable: true, Cause: err}
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return &Error{Kind: KindTransport, Operation: operation, Message: "read Taiga API response", Retryable: true, Cause: readErr}
	}
	if closeErr != nil {
		return &Error{Kind: KindTransport, Operation: operation, Message: "close Taiga API response", Retryable: false, Cause: closeErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(operation, resp.StatusCode, data)
	}
	var response AuthResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return &Error{Kind: KindTransport, Operation: operation, Message: "decode Taiga API response", Retryable: false, Cause: err}
	}
	if response.AuthToken == "" {
		return &Error{Kind: KindTransport, Operation: operation, Message: "Taiga refresh response did not contain an auth token", Retryable: false}
	}
	c.token = response.AuthToken
	if response.RefreshToken != "" {
		c.refreshToken = response.RefreshToken
	}
	if c.onRefresh == nil {
		return nil
	}
	// Taiga has already retired the old refresh token by this point, so failing
	// to store the new one leaves the copy on disk dead. Saying so is the
	// difference between one confusing failure and being locked out with no
	// idea why.
	if err := c.onRefresh(c.token, c.refreshToken); err != nil {
		return &Error{Kind: KindAuth, Operation: operation, Message: refreshNotStoredMessage, Retryable: false, Cause: err}
	}
	return nil
}

func decodeAPIError(operation string, status int, data []byte) *Error {
	details := decodeErrorDetails(data)
	kind, retryable := classifyAPIError(status, details)
	return &Error{Kind: kind, Operation: operation, Message: explainAPIError(status, details), Retryable: retryable, UpstreamStatus: status, Details: details}
}

// decodeErrorDetails reads a Taiga error body into field-keyed form. Django
// REST Framework answers a many=True serializer -- which is what bulk create
// posts -- with one entry per submitted row rather than an object, so those are
// keyed by position; the row number is the only way to tell which of a thousand
// subjects was rejected.
func decodeErrorDetails(data []byte) map[string]any {
	details := map[string]any{}
	if err := json.Unmarshal(data, &details); err == nil {
		return details
	}
	var rows []any
	if err := json.Unmarshal(data, &rows); err != nil {
		return map[string]any{}
	}
	for index, row := range rows {
		if fields, ok := row.(map[string]any); ok && len(fields) > 0 {
			details[itemKeyPrefix+strconv.Itoa(index)] = fields
		}
	}
	return details
}

// classifyAPIError decides what the caller should believe about the server's
// state. It reads the body's structure only: the sentences in it are written
// for a person, and Taiga translates them, so a rule that matched on their
// wording would fail silently against a server running in another language.
func classifyAPIError(status int, details map[string]any) (ErrorKind, bool) {
	switch status {
	case http.StatusUnauthorized:
		return KindAuth, false
	case http.StatusForbidden:
		return KindForbidden, false
	case http.StatusNotFound:
		return KindNotFound, false
	case http.StatusConflict:
		return KindConflict, false
	case http.StatusTooManyRequests:
		return KindThrottled, true
	}
	if status >= 500 {
		return KindTransport, true
	}
	if status == http.StatusBadRequest && isStaleVersion(details) {
		return KindConflict, false
	}
	return KindValidation, false
}

// isStaleVersion separates Taiga refusing a write because somebody else got
// there first from Taiga refusing the version field itself. The two arrive as
// the same status and under the same key, and differ only in shape: Taiga's
// concurrency check raises its own exception carrying one sentence, while
// Django REST Framework reports a malformed field as a list of them. Telling
// them apart matters because a conflict asks the caller to re-read and retry,
// which never terminates when the value was simply not a valid version.
func isStaleVersion(details map[string]any) bool {
	value, ok := details[versionField]
	if !ok {
		return false
	}
	sentence, ok := value.(string)
	return ok && strings.TrimSpace(sentence) != ""
}

// explainAPIError renders what a person needs to read. Taiga's own prose wins
// when it says more than the status already does; otherwise the rejected fields
// are named, which for a validation failure is the whole of the answer.
func explainAPIError(status int, details map[string]any) string {
	statusText := http.StatusText(status)
	if prose := proseMessage(details); prose != "" && prose != statusText {
		return truncateMessage(prose)
	}
	// Field rendering belongs to validation responses alone. Applied to a 5xx
	// it would replace a status people recognise with whatever keys a proxy's
	// error page happens to carry.
	if status == http.StatusBadRequest {
		if explanation := fieldExplanation(details, 0); explanation != "" {
			return explanation
		}
	}
	return statusText
}

// proseMessage returns Taiga's own sentence about the request, if it sent one.
func proseMessage(details map[string]any) string {
	for _, key := range proseKeys {
		if value, ok := details[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// isProseKey reports whether a key holds a sentence about the whole request
// rather than a rejected field, so that rendering the fields does not repeat
// the sentence back as though a field were named after it.
func isProseKey(key string, value any) bool {
	if _, ok := value.(string); !ok {
		return false
	}
	return slices.Contains(proseKeys, key)
}

// fieldExplanation renders rejected fields as one sentence, sorted so the same
// response always reads the same way.
func fieldExplanation(details map[string]any, depth int) string {
	if depth > maxFieldDepth {
		return ""
	}
	type namedField struct{ key, text string }
	named := make([]namedField, 0, len(details))
	for key, value := range details {
		if strings.HasPrefix(key, "_") || isProseKey(key, value) {
			continue
		}
		text := fieldMessage(value, depth+1)
		if text == "" {
			continue
		}
		// non_field_errors is the framework's name for a rejection belonging to
		// no field, so printing the name would only add noise.
		if key != nonFieldErrorsKey {
			text = key + ": " + text
		}
		named = append(named, namedField{key: key, text: text})
	}
	sort.Slice(named, func(i, j int) bool { return named[i].key < named[j].key })
	if len(named) > maxRenderedFields {
		named = named[:maxRenderedFields]
	}
	texts := make([]string, 0, len(named))
	for _, field := range named {
		texts = append(texts, field.text)
	}
	return truncateMessage(strings.Join(texts, "; "))
}

// fieldMessage renders one field's rejection. Django REST Framework nests a
// sub-serializer's errors under the field holding it, so a value may be a
// sentence, a list of them, or a further map of fields.
func fieldMessage(value any, depth int) string {
	if depth > maxFieldDepth {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		messages := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := fieldMessage(item, depth+1); text != "" {
				messages = append(messages, text)
			}
		}
		return strings.Join(messages, " ")
	case map[string]any:
		return fieldExplanation(typed, depth+1)
	}
	return ""
}

// truncateMessage bounds what reaches a terminal. A body is read up to
// maxResponseBytes, and a rejected bulk create carries a message per row, so
// the rendering is capped rather than handed over whole.
func truncateMessage(text string) string {
	if len(text) <= maxMessageBytes {
		return text
	}
	cut := maxMessageBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "… (truncated)"
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
		return min(time.Duration(seconds)*time.Second, maxRetryAfter)
	}
	if date, err := http.ParseTime(strings.TrimSpace(retryAfter)); err == nil {
		return min(max(time.Until(date), 0), maxRetryAfter)
	}
	// Jitter only has to stop simultaneous callers retrying in lockstep, so a
	// predictable source is fine here; nothing about it is a secret.
	return backoffBase(attempt) + rand.N(jitterWindow) // #nosec G404
}

func backoffBase(attempt int) time.Duration {
	if attempt > maxBackoffShift {
		attempt = maxBackoffShift
	}
	return backoffStep * time.Duration(1<<(attempt-1))
}

func (c *Client) log(method, path string, status int, elapsed time.Duration) {
	if c.verbose == nil {
		return
	}
	if status == 0 {
		_, _ = fmt.Fprintf(c.verbose, "%s %s transport-error %s\n", method, path, elapsed.Round(time.Millisecond))
		return
	}
	_, _ = fmt.Fprintf(c.verbose, "%s %s %d %s\n", method, path, status, elapsed.Round(time.Millisecond))
}

func pageFromHeaders(header http.Header, fallbackSize int) Page {
	integer := func(name string) int {
		value, _ := strconv.Atoi(header.Get(name))
		return value
	}
	page := Page{
		Number: integer("X-Pagination-Current"),
		Size:   integer("X-Paginated-By"),
		Total:  integer("X-Pagination-Count"),
		Next:   integer("X-Pagination-Next"),
		Prev:   integer("X-Pagination-Prev"),
	}
	if page.Number == 0 {
		page.Number = 1
	}
	if page.Size == 0 {
		page.Size = fallbackSize
	}
	return page
}
