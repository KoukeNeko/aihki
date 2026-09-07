package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/aihki/internal/completioncache"
	"github.com/KoukeNeko/aihki/internal/config"
	"github.com/KoukeNeko/aihki/internal/credential"
	"github.com/KoukeNeko/aihki/internal/output"
	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

const (
	environmentPrefix       = "AIHKI_"
	legacyEnvironmentPrefix = "TAIGA_"
)

type App struct {
	In              io.Reader
	Out             io.Writer
	Err             io.Writer
	HTTPClient      *http.Client
	Config          *config.Store
	GitLocal        *config.GitLocal
	Credentials     credential.Store
	CompletionCache *completioncache.Store
	Getenv          func(string) string
	Cwd             string
	// StdinTTY reports whether input is a terminal, which is what decides
	// whether a question may be asked at all. It is left unset outside tests,
	// where input is a buffer that could never answer one.
	StdinTTY func() bool

	global globalOptions
}

type globalOptions struct {
	Profile string
	APIURL  string
	Project string
	JSON    bool
	Fields  []string
	NoInput bool
	NoColor bool
	Quiet   bool
	Verbose bool
}

type Settings struct {
	Profile      string `json:"profile"`
	APIURL       string `json:"api_url"`
	Project      string `json:"project,omitempty"`
	Token        string `json:"-"`
	RefreshToken string `json:"-"`
}

func New() (*App, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get current directory: %w", err)
	}
	// No overall deadline on the client: the request layer bounds each JSON
	// attempt itself and watches a transfer for stalling, so an attachment or
	// a dump is as long as it is rather than as long as thirty seconds allow.
	return &App{
		In:              os.Stdin,
		Out:             os.Stdout,
		Err:             os.Stderr,
		HTTPClient:      &http.Client{},
		Config:          config.NewStore(path),
		GitLocal:        config.NewGitLocal(cwd),
		Credentials:     credential.NewKeyringStore(),
		CompletionCache: completioncache.NewStore(completioncache.DefaultPath(path)),
		Getenv:          os.Getenv,
		Cwd:             cwd,
	}, nil
}

func (a *App) Execute(ctx context.Context, args []string) int {
	root := a.rootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitSuccess
	}
	known, body := classifyError(err)
	renderer := a.renderer()
	_ = renderer.Failure(body)
	return known.ExitCode
}

// flushTable writes a table out and, when the page it holds is only part of
// the list, says so. A table that stops at the page size looks exactly like a
// list that ends there, and a reader who cannot tell the two apart draws
// conclusions from a fraction of the data. The notice goes to stderr so that
// the table itself stays pipeable, and every command that paginates has
// --limit, which is why that is the flag named. JSON output says none of this
// because it already carries the page.
func (a *App) flushTable(writer *tabwriter.Writer, shown, total int) error {
	if err := writer.Flush(); err != nil {
		return err
	}
	if a.global.Quiet || total <= shown {
		return nil
	}
	_, _ = fmt.Fprintf(a.Err, "showing %d of %d; use --limit to see more\n", shown, total)
	return nil
}

func (a *App) renderer() output.Renderer {
	return output.Renderer{Out: a.Out, Err: a.Err, JSON: a.global.JSON, Fields: a.global.Fields, Quiet: a.global.Quiet}
}

func (a *App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "aihki",
		Short:         "Manage Taiga projects from the command line",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if len(a.global.Fields) > 0 && !a.global.JSON {
				return usageError("--fields requires --json")
			}
			if a.Getenv("NO_COLOR") != "" {
				a.global.NoColor = true
			}
			return nil
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err.Error()) })
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	flags := root.PersistentFlags()
	flags.StringVar(&a.global.Profile, "profile", "", "Taiga profile to use")
	flags.StringVar(&a.global.APIURL, "api-url", "", "complete Taiga API base URL")
	flags.StringVarP(&a.global.Project, "project", "p", "", "Taiga project slug")
	flags.BoolVar(&a.global.JSON, "json", false, "emit the versioned JSON contract")
	flags.StringSliceVar(&a.global.Fields, "fields", nil, "comma-separated JSON fields to include")
	flags.BoolVar(&a.global.NoInput, "no-input", false, "never prompt for input")
	flags.BoolVar(&a.global.NoColor, "no-color", false, "disable color output")
	flags.BoolVarP(&a.global.Quiet, "quiet", "q", false, "suppress non-essential human output")
	flags.BoolVarP(&a.global.Verbose, "verbose", "v", false, "print redacted HTTP diagnostics to stderr")
	_ = root.RegisterFlagCompletionFunc("profile", a.completeProfiles)
	_ = root.RegisterFlagCompletionFunc("project", a.completeProjects)
	root.AddCommand(
		a.versionCommand(),
		a.doctorCommand(),
		a.authCommand(),
		a.configCommand(),
		a.projectCommand(),
		a.memberCommand(),
		a.roleCommand(),
		a.webhookCommand(),
		a.customFieldCommand(),
		a.metadataCommand(),
		a.dueDateCommand(),
		a.swimlaneCommand(),
		a.tagCommand(),
		a.notificationCommand(),
		a.applicationCommand(),
		a.storageCommand(),
		a.integrationCommand(),
		a.epicCommand(),
		a.issueCommand(),
		a.storyCommand(),
		a.taskCommand(),
		a.sprintCommand(),
		a.wikiCommand(),
		a.wikiLinkCommand(),
		a.searchCommand(),
		a.timelineCommand(),
		a.statsCommand(),
		a.batchCommand(),
		a.commentCommand(),
		a.csvCommand(),
		a.attachmentCommand(),
		a.schemaCommand(),
		a.completionCommand(root),
	)
	return root
}

func (a *App) resolveSettings(ctx context.Context) (Settings, config.File, error) {
	cfg, err := a.Config.Load()
	if err != nil {
		return Settings{}, config.File{}, err
	}
	local := config.LocalValues{}
	if a.GitLocal != nil {
		values, localErr := a.GitLocal.Load(ctx)
		if localErr == nil {
			local = values
		} else if !errors.Is(localErr, config.ErrNotGitRepository) {
			return Settings{}, config.File{}, localErr
		}
	}
	profileName := firstNonEmpty(a.global.Profile, a.env("PROFILE"), local.Profile, cfg.CurrentProfile, config.DefaultProfileName())
	profileName, err = config.NormalizeProfileName(profileName)
	if err != nil {
		return Settings{}, config.File{}, validationError("invalid_profile", err.Error())
	}
	profile := cfg.Profiles[profileName]
	apiURL := firstNonEmpty(a.global.APIURL, a.env("API_URL"), profile.APIURL)
	if apiURL != "" {
		apiURL, err = taiga.NormalizeAPIURL(apiURL)
		if err != nil {
			return Settings{}, config.File{}, validationError("invalid_api_url", err.Error())
		}
	}
	project := firstNonEmpty(a.global.Project, a.env("PROJECT"), local.Project, profile.Project)
	token := strings.TrimSpace(a.env("TOKEN"))
	refreshToken := ""
	if token == "" && apiURL != "" && a.Credentials != nil {
		tokens, credentialErr := a.Credentials.Get(credential.Account(profileName, apiURL))
		if credentialErr == nil {
			token = tokens.AuthToken
			refreshToken = tokens.RefreshToken
		} else if !errors.Is(credentialErr, credential.ErrNotFound) {
			return Settings{}, config.File{}, credentialErr
		}
	}
	return Settings{Profile: profileName, APIURL: apiURL, Project: project, Token: token, RefreshToken: refreshToken}, cfg, nil
}

func (a *App) client(ctx context.Context, requireToken bool) (*taiga.Client, Settings, error) {
	settings, _, err := a.resolveSettings(ctx)
	if err != nil {
		return nil, Settings{}, err
	}
	if settings.APIURL == "" {
		return nil, Settings{}, validationError("missing_api_url", "no Taiga API URL configured; run `aihki auth login --host <url>` or pass --api-url")
	}
	if requireToken && settings.Token == "" {
		return nil, Settings{}, authRequired("no Taiga credential available; run `aihki auth login` or set AIHKI_TOKEN")
	}
	options := []taiga.ClientOption{taiga.WithHTTPClient(a.HTTPClient), taiga.WithToken(settings.Token)}
	if settings.RefreshToken != "" && a.Credentials != nil {
		account := credential.Account(settings.Profile, settings.APIURL)
		options = append(options, taiga.WithRefreshToken(settings.RefreshToken, func(authToken, refreshToken string) error {
			return a.Credentials.Set(account, credential.Tokens{AuthToken: authToken, RefreshToken: refreshToken})
		}))
	}
	if a.global.Verbose {
		options = append(options, taiga.WithVerbose(a.Err))
	}
	client, err := taiga.NewClient(settings.APIURL, options...)
	return client, settings, err
}

// env reads a setting from the current environment variable, falling back to
// the pre-rename name so existing shells and CI jobs keep working. The old
// names stay supported rather than failing silently, which is the failure mode
// that would be hardest for a user to diagnose.
func (a *App) env(name string) string {
	if value := strings.TrimSpace(a.Getenv(environmentPrefix + name)); value != "" {
		return value
	}
	return strings.TrimSpace(a.Getenv(legacyEnvironmentPrefix + name))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
