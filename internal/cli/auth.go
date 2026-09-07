package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/KoukeNeko/aihki/internal/config"
	"github.com/KoukeNeko/aihki/internal/credential"
	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *App) authCommand() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Authenticate with Taiga"}
	command.AddCommand(a.authLoginCommand(), a.authLogoutCommand(), a.authStatusCommand())
	return command
}

// loginOptions is what the login command's flags say.
type loginOptions struct {
	SiteURL   string
	Username  string
	WithToken bool
}

// loginTarget is where a login goes: the API that takes the credential and,
// when the API was found through the web app, the web app's address.
type loginTarget struct {
	apiURL string
	site   string
}

// signInMethod is how the person's account signs in to Taiga.
type signInMethod int

const (
	signInWithPassword signInMethod = iota
	signInWithProvider
	signInWithToken
)

func (a *App) authLoginCommand() *cobra.Command {
	var options loginOptions
	command := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and save a Taiga credential",
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.login(cmd.Context(), options) },
	}
	addSiteURLFlag(command, &options.SiteURL)
	command.Flags().StringVarP(&options.Username, "username", "u", "", "Taiga username or email")
	command.Flags().BoolVar(&options.WithToken, "with-token", false, "read a bearer token from standard input, or prompt for it at a terminal")
	return command
}

// addSiteURLFlag binds --url, and keeps --host working for scripts written
// before the rename: what the flag takes is a URL with a scheme and a path,
// which "host" misnamed.
func addSiteURLFlag(command *cobra.Command, target *string) {
	command.Flags().StringVar(target, "url", "", "any page inside the Taiga web app, or the API's address")
	command.Flags().StringVar(target, "host", "", "")
	_ = command.Flags().MarkDeprecated("host", "use --url")
}

func (a *App) login(ctx context.Context, options loginOptions) error {
	if options.SiteURL != "" && a.global.APIURL != "" {
		return usageError("--url and --api-url are mutually exclusive")
	}
	settings, cfg, err := a.resolveSettings(ctx)
	if err != nil {
		return err
	}
	target, err := a.loginTarget(ctx, options.SiteURL, settings)
	if err != nil {
		return err
	}
	clientOptions := []taiga.ClientOption{taiga.WithHTTPClient(a.HTTPClient)}
	if a.global.Verbose {
		clientOptions = append(clientOptions, taiga.WithVerbose(a.Err))
	}
	client, err := taiga.NewClient(target.apiURL, clientOptions...)
	if err != nil {
		return err
	}
	tokens, user, err := a.authenticate(ctx, client, target, options)
	if err != nil {
		return err
	}
	updateProfile(&cfg, settings.Profile, func(profile *config.Profile) { profile.APIURL = target.apiURL })
	cfg.CurrentProfile = settings.Profile
	if err := a.Config.Save(cfg); err != nil {
		return err
	}
	if err := a.Credentials.Set(credential.Account(settings.Profile, target.apiURL), tokens); err != nil {
		return err
	}
	result := map[string]any{"profile": settings.Profile, "api_url": target.apiURL, "user": user, "refresh_token_stored": tokens.RefreshToken != ""}
	if a.global.JSON {
		return a.renderer().Data(result)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "Logged in to %s as %s (profile %s)\n", target.apiURL, user.Username, settings.Profile)
		if tokens.RefreshToken == "" {
			_, _ = fmt.Fprintln(a.Out, "No refresh token was given, so this login lasts until the token expires.")
		}
	}
	return nil
}

// loginTarget resolves where the login goes. A URL on the command line is
// discovered from; a configured API URL is reused; otherwise the terminal is
// asked, and a script is told what to pass.
func (a *App) loginTarget(ctx context.Context, siteURL string, settings Settings) (loginTarget, error) {
	if siteURL == "" && settings.APIURL != "" {
		return loginTarget{apiURL: settings.APIURL}, nil
	}
	if siteURL == "" {
		if a.global.NoInput || !a.stdinTTY() {
			return loginTarget{}, validationError("missing_api_url", "a Taiga URL is required in non-interactive mode; pass --url with any page inside the Taiga web app, or --api-url")
		}
		var err error
		siteURL, err = a.askSite()
		if err != nil {
			return loginTarget{}, err
		}
	}
	front, err := a.discoverOrOfferHosted(ctx, siteURL)
	if err != nil {
		return loginTarget{}, err
	}
	return loginTarget{apiURL: front.API, site: front.Site}, nil
}

// discoverOrOfferHosted finds the Taiga behind siteURL. When the site is under
// the hosted Taiga's domain but is not the app -- the forum, most often -- a
// terminal is offered the app instead, since that is a fact about the domain
// rather than a guess; the person answers before anything else is contacted,
// and a script still gets the error, because nothing may choose a destination
// for it.
func (a *App) discoverOrOfferHosted(ctx context.Context, siteURL string) (taiga.FrontConfig, error) {
	front, err := taiga.DiscoverAPI(ctx, a.HTTPClient, siteURL)
	if err == nil {
		return front, nil
	}
	hosted, ok := taiga.HostedTaigaFor(siteURL)
	if !ok || !isWrongSite(err) || a.global.NoInput || !a.stdinTTY() {
		return taiga.FrontConfig{}, err
	}
	_, _ = fmt.Fprintf(a.Err, "%s is not a Taiga web app or API.\n", siteURL)
	accepted, confirmErr := a.confirm("Use " + hosted + " instead?")
	if confirmErr != nil {
		return taiga.FrontConfig{}, confirmErr
	}
	if !accepted {
		return taiga.FrontConfig{}, err
	}
	return taiga.DiscoverAPI(ctx, a.HTTPClient, hosted)
}

// isWrongSite reports whether discovery reached the site and found no Taiga
// there, as opposed to failing to reach it at all.
func isWrongSite(err error) bool {
	var apiErr *taiga.Error
	return errors.As(err, &apiErr) && apiErr.Kind != taiga.KindTransport
}

// askSite asks for the Taiga site as one question, the same for every site.
// The hosted Taiga is the default, so that its users press Enter and never
// have to know a URL, and the default is also the example of what to paste.
func (a *App) askSite() (string, error) {
	return a.readLineOr("Taiga URL (paste any page from inside the Taiga web app)", taiga.HostedTaigaApp)
}

// authenticate obtains a credential for target. Piped token input stays
// silent for scripts; at a terminal the destination is shown before any
// secret is asked for, and the account's way of signing in is asked before
// a password is, since an account that signs in through a provider has none.
func (a *App) authenticate(ctx context.Context, client *taiga.Client, target loginTarget, options loginOptions) (credential.Tokens, taiga.User, error) {
	if options.WithToken && (a.global.NoInput || !a.stdinTTY()) {
		text, err := a.readTokenFromStdin()
		if err != nil {
			return credential.Tokens{}, taiga.User{}, err
		}
		return a.loginWithToken(ctx, client, text)
	}
	if a.global.NoInput || !a.stdinTTY() {
		return credential.Tokens{}, taiga.User{}, validationError("input_required", "interactive login requires a TTY; use --with-token or AIHKI_TOKEN for automation")
	}
	a.showLoginTarget(target)
	method := signInWithPassword
	switch {
	case options.WithToken:
		method = signInWithToken
	case options.Username == "":
		choice, err := a.readChoice("How do you sign in to Taiga?", []string{"Username and password", "GitHub, Google or another sign-in provider", "An existing Taiga token"})
		if err != nil {
			return credential.Tokens{}, taiga.User{}, err
		}
		method = signInMethod(choice)
	}
	if method == signInWithPassword {
		return a.loginWithPassword(ctx, client, target, options.Username)
	}
	if method == signInWithProvider {
		a.explainProviderSignIn()
	}
	text, err := a.readSecret("Token: ", tokenSecret)
	if err != nil {
		return credential.Tokens{}, taiga.User{}, err
	}
	return a.loginWithToken(ctx, client, text)
}

// showLoginTarget puts the destination in front of the person before any
// credential is asked for.
func (a *App) showLoginTarget(target loginTarget) {
	if target.site != "" {
		_, _ = fmt.Fprintf(a.Err, "Taiga: %s\nAPI:   %s\n", target.site, target.apiURL)
		return
	}
	_, _ = fmt.Fprintf(a.Err, "Taiga API: %s\n", target.apiURL)
}

// consoleCopySnippet copies both of the web app's tokens as one JSON object,
// so that the refresh token travels with the access token and the login can
// renew itself.
const consoleCopySnippet = `copy(JSON.stringify({auth_token: JSON.parse(localStorage.token), refresh: JSON.parse(localStorage.refresh)}))`

func (a *App) explainProviderSignIn() {
	_, _ = fmt.Fprint(a.Err, "An account that signs in with GitHub, Google or another provider has no Taiga password.\n"+
		"Sign in on the web, open the browser's JavaScript console on that page, run\n"+
		"  "+consoleCopySnippet+"\n"+
		"and paste the result here.\n")
}

func (a *App) loginWithPassword(ctx context.Context, client *taiga.Client, target loginTarget, username string) (credential.Tokens, taiga.User, error) {
	var err error
	if username == "" {
		username, err = a.readLine("Username or email: ")
		if err != nil {
			return credential.Tokens{}, taiga.User{}, err
		}
	}
	password, err := a.readSecret("Password: ", passwordSecret)
	if err != nil {
		return credential.Tokens{}, taiga.User{}, err
	}
	response, err := client.Login(ctx, username, password)
	if err != nil {
		// A refused password is what a provider-backed account gets every
		// time, so the way in for such an account travels with the refusal.
		var apiErr *taiga.Error
		if errors.As(err, &apiErr) && apiErr.Kind == taiga.KindAuth {
			apiErr.Message += "; an account that signs in to Taiga with GitHub, Google or another provider has no password, so sign in with its token instead: " + a.tokenLoginHint(target)
		}
		return credential.Tokens{}, taiga.User{}, err
	}
	tokens := credential.Tokens{AuthToken: response.AuthToken, RefreshToken: response.RefreshToken}
	user := taiga.User{ID: response.ID, Username: response.Username, FullName: response.FullName}
	return tokens, user, nil
}

func (a *App) loginWithToken(ctx context.Context, client *taiga.Client, text string) (credential.Tokens, taiga.User, error) {
	tokens, err := parseTokenInput(text)
	if err != nil {
		return credential.Tokens{}, taiga.User{}, err
	}
	client.SetToken(tokens.AuthToken)
	// A pasted refresh token is good for the login itself, not only for
	// later: an access token that expired between the browser and the
	// terminal is refreshed here, and what gets stored is the rotated pair.
	if tokens.RefreshToken != "" {
		client.SetRefreshToken(tokens.RefreshToken, func(authToken, refreshToken string) error {
			tokens.AuthToken, tokens.RefreshToken = authToken, refreshToken
			return nil
		})
	}
	user, err := client.Me(ctx)
	if err != nil {
		return credential.Tokens{}, taiga.User{}, err
	}
	return tokens, user, nil
}

// parseTokenInput reads what --with-token was given: a bare access token, or
// the JSON object the web app holds, {"auth_token": ..., "refresh": ...},
// which is the shape of Taiga's own login answer and carries the refresh
// token that lets the login renew itself. "token" is taken for auth_token
// too, since that is the key the browser shows.
func parseTokenInput(text string) (credential.Tokens, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return credential.Tokens{AuthToken: text}, nil
	}
	var pasted struct {
		AuthToken string `json:"auth_token"`
		Token     string `json:"token"`
		Refresh   string `json:"refresh"`
	}
	if err := json.Unmarshal([]byte(text), &pasted); err != nil {
		return credential.Tokens{}, validationError("invalid_token", "the token must be a bearer token or a JSON object with auth_token and refresh")
	}
	tokens := credential.Tokens{AuthToken: firstNonEmpty(pasted.AuthToken, pasted.Token), RefreshToken: strings.TrimSpace(pasted.Refresh)}
	if tokens.AuthToken == "" {
		return credential.Tokens{}, validationError("empty_token", "the JSON object names no auth_token")
	}
	return tokens, nil
}

func (a *App) tokenLoginHint(target loginTarget) string {
	return "aihki auth login --url " + firstNonEmpty(target.site, target.apiURL) + " --with-token"
}

func (a *App) authLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved credential for the current profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, _, err := a.resolveSettings(cmd.Context())
			if err != nil {
				return err
			}
			if settings.APIURL == "" {
				return validationError("missing_api_url", "current profile has no API URL")
			}
			if err := a.Credentials.Delete(credential.Account(settings.Profile, settings.APIURL)); err != nil {
				return err
			}
			result := map[string]any{"profile": settings.Profile, "logged_out": true}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Logged out profile %s\n", settings.Profile)
			}
			return nil
		},
	}
}

func (a *App) authStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, settings, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			user, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			result := map[string]any{"profile": settings.Profile, "api_url": settings.APIURL, "project": settings.Project, "user": user, "authenticated": true}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			_, _ = fmt.Fprintf(a.Out, "Authenticated to %s as %s (profile %s)\n", settings.APIURL, user.Username, settings.Profile)
			return nil
		},
	}
}

func (a *App) stdinTTY() bool {
	file, ok := a.In.(*os.File)
	// x/term takes an int, and a descriptor is small and non-negative.
	return ok && term.IsTerminal(int(file.Fd())) // #nosec G115
}

func (a *App) readLine(prompt string) (string, error) {
	_, _ = fmt.Fprint(a.Err, prompt)
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", validationError("empty_input", "input cannot be empty")
	}
	return line, nil
}

// confirm asks a yes-or-no question; Enter means yes.
func (a *App) confirm(question string) (bool, error) {
	_, _ = fmt.Fprintf(a.Err, "%s [Y/n]: ", question)
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read input: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes", nil
}

// readLineOr reads one line, showing fallback in the prompt and returning it
// when the line is empty.
func (a *App) readLineOr(prompt, fallback string) (string, error) {
	_, _ = fmt.Fprintf(a.Err, "%s [%s]: ", prompt, fallback)
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	if line = strings.TrimSpace(line); line != "" {
		return line, nil
	}
	return fallback, nil
}

// readChoice prints numbered options and reads the number of one. Enter alone
// takes the first, which is why the first is the likeliest.
func (a *App) readChoice(question string, options []string) (int, error) {
	_, _ = fmt.Fprintln(a.Err, question)
	for index, option := range options {
		_, _ = fmt.Fprintf(a.Err, "  %d) %s\n", index+1, option)
	}
	_, _ = fmt.Fprint(a.Err, "Choice [1]: ")
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("read input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}
	number, err := strconv.Atoi(line)
	if err != nil || number < 1 || number > len(options) {
		return 0, validationError("invalid_choice", fmt.Sprintf("choose a number between 1 and %d", len(options)))
	}
	return number - 1, nil
}

// maxTokenBytes bounds a token read from a pipe, so that a stream that never
// ends cannot exhaust memory.
const maxTokenBytes = 64 << 10

// secretWording is what a command calls the secret it is reading, in the
// errors it returns for it.
type secretWording struct {
	name         string
	emptyCode    string
	emptyMessage string
}

var (
	passwordSecret = secretWording{name: "password", emptyCode: "empty_password", emptyMessage: "password cannot be empty"}
	tokenSecret    = secretWording{name: "token", emptyCode: "empty_token", emptyMessage: "--with-token requires a token"}
)

// readTokenFromStdin takes the token --with-token was promised on a pipe:
// whatever arrives before the end of input.
func (a *App) readTokenFromStdin() (string, error) {
	data, err := io.ReadAll(io.LimitReader(a.In, maxTokenBytes))
	if err != nil {
		return "", fmt.Errorf("read token from stdin: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", validationError("empty_token", "--with-token requires a token on stdin")
	}
	return token, nil
}

// readSecret reads one line at the terminal without echoing it, so that
// pasting a secret and pressing Enter is enough and it stays out of the
// scrollback.
func (a *App) readSecret(prompt string, wording secretWording) (string, error) {
	file, ok := a.In.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) { // #nosec G115 -- see stdinTTY
		return "", validationError("input_required", wording.name+" input requires a TTY")
	}
	_, _ = fmt.Fprint(a.Err, prompt)
	data, err := term.ReadPassword(int(file.Fd())) // #nosec G115 -- see stdinTTY
	_, _ = fmt.Fprintln(a.Err)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", wording.name, err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", validationError(wording.emptyCode, wording.emptyMessage)
	}
	return secret, nil
}

// ensureProfile creates the named profile when it is absent so that selecting a
// profile records it even before any of its settings are known.
func ensureProfile(cfg *config.File, name string) {
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]config.Profile{}
	}
	if _, ok := cfg.Profiles[name]; !ok {
		cfg.Profiles[name] = config.Profile{}
	}
}

// updateProfile applies change to the named profile and stores the result,
// creating the profile when it is absent.
func updateProfile(cfg *config.File, name string, change func(*config.Profile)) {
	ensureProfile(cfg, name)
	profile := cfg.Profiles[name]
	change(&profile)
	cfg.Profiles[name] = profile
}
