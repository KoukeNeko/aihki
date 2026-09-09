package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/KoukeNeko/taiga-cli/internal/config"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	buildversion "github.com/KoukeNeko/taiga-cli/internal/version"
	"github.com/spf13/cobra"
)

type diagnosticManifest struct {
	Format    int      `json:"format"`
	CreatedAt string   `json:"created_at"`
	Privacy   string   `json:"privacy"`
	Files     []string `json:"files"`
}

type diagnosticConfig struct {
	Status              string `json:"status"`
	ProfileCount        int    `json:"profile_count"`
	ConfiguredAPIURLs   int    `json:"configured_api_urls"`
	ConfiguredProjects  int    `json:"configured_projects"`
	CurrentHasAPIURL    bool   `json:"current_has_api_url"`
	CurrentHasProject   bool   `json:"current_has_project"`
	CredentialPresent   bool   `json:"credential_present"`
	RefreshTokenPresent bool   `json:"refresh_token_present"`
	EnvironmentTokenSet bool   `json:"environment_token_set"`
	GitRepository       bool   `json:"git_repository"`
	GitLocalProfileSet  bool   `json:"git_local_profile_set"`
	GitLocalProjectSet  bool   `json:"git_local_project_set"`
}

type diagnosticCheck struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Code           string `json:"code,omitempty"`
	Retryable      bool   `json:"retryable,omitempty"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	LatencyMS      int64  `json:"latency_ms,omitempty"`
}

type diagnosticSystem struct {
	CLIVersion string `json:"cli_version"`
	Commit     string `json:"commit"`
	BuildDate  string `json:"build_date"`
	GoVersion  string `json:"go_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Contract   int    `json:"contract"`
}

func (a *App) doctorBundleCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use: "bundle <output.zip>", Short: "Create an opt-in diagnostic bundle without identifiers or secrets", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolve diagnostic bundle path: %w", err)
			}
			if filepath.Ext(path) != ".zip" {
				return usageError("diagnostic bundle output must use a .zip extension")
			}
			if !force {
				if _, err := os.Stat(path); err == nil {
					return validationError("output_exists", "diagnostic bundle output already exists; pass --force to replace it")
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("inspect diagnostic bundle output: %w", err)
				}
			}
			files, sensitive, err := a.buildDiagnosticBundle(cmd.Context())
			if err != nil {
				return err
			}
			if leaked := containsSensitiveValue(files, sensitive); leaked != "" {
				return validationError("diagnostic_redaction_failed", "diagnostic bundle contained a sensitive runtime value and was not written")
			}
			data, err := encodeDiagnosticZip(files, time.Now().UTC())
			if err != nil {
				return err
			}
			if err := writeDiagnosticBundle(path, data, force); err != nil {
				return err
			}
			result := map[string]any{"path": path, "bytes": len(data), "files": sortedDiagnosticNames(files), "redacted": true, "uploaded": false}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Created redacted diagnostic bundle: %s\n", path)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&force, "force", false, "replace an existing bundle atomically")
	return command
}

func (a *App) buildDiagnosticBundle(ctx context.Context) (map[string][]byte, []string, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	files := map[string][]byte{}
	system := diagnosticSystem{CLIVersion: buildversion.Version, Commit: buildversion.Commit, BuildDate: buildversion.BuildDate, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Contract: 1}
	settings, cfg, resolveErr := a.resolveSettings(ctx)
	configReport := diagnosticConfig{Status: "ok", ProfileCount: len(cfg.Profiles), EnvironmentTokenSet: a.env("TOKEN") != ""}
	sensitive := []string{}
	for _, profile := range cfg.Profiles {
		sensitive = append(sensitive, profile.APIURL, profile.Project)
		if profile.APIURL != "" {
			configReport.ConfiguredAPIURLs++
		}
		if profile.Project != "" {
			configReport.ConfiguredProjects++
		}
	}
	for name := range cfg.Profiles {
		sensitive = append(sensitive, name)
	}
	if resolveErr != nil {
		configReport.Status = "error"
	} else {
		configReport.CurrentHasAPIURL = settings.APIURL != ""
		configReport.CurrentHasProject = settings.Project != ""
		configReport.CredentialPresent = settings.Token != ""
		configReport.RefreshTokenPresent = settings.RefreshToken != ""
		sensitive = append(sensitive, settings.Token, settings.RefreshToken, settings.APIURL, settings.Project, settings.Profile)
	}
	if a.GitLocal != nil {
		if local, err := a.GitLocal.Load(ctx); err == nil {
			configReport.GitRepository = true
			configReport.GitLocalProfileSet = local.Profile != ""
			configReport.GitLocalProjectSet = local.Project != ""
			sensitive = append(sensitive, local.Profile, local.Project)
		} else if !errors.Is(err, config.ErrNotGitRepository) {
			configReport.Status = "error"
		}
	}
	checks, observedSensitive := a.diagnosticChecks(ctx, settings, resolveErr)
	sensitive = append(sensitive, observedSensitive...)
	files["system.json"] = mustDiagnosticJSON(system)
	files["config.json"] = mustDiagnosticJSON(configReport)
	files["checks.json"] = mustDiagnosticJSON(checks)
	names := []string{"checks.json", "config.json", "manifest.json", "system.json"}
	manifest := diagnosticManifest{Format: 1, CreatedAt: createdAt, Privacy: "No URLs, hostnames, usernames, project names, paths, environment values, request bodies, logs, or credentials are included.", Files: names}
	files["manifest.json"] = mustDiagnosticJSON(manifest)
	return files, sensitive, nil
}

func (a *App) diagnosticChecks(ctx context.Context, settings Settings, resolveErr error) ([]diagnosticCheck, []string) {
	checks := []diagnosticCheck{}
	observedSensitive := []string{}
	if resolveErr != nil {
		return []diagnosticCheck{{Name: "configuration", Status: "error", Code: "configuration_error"}, {Name: "api", Status: "pending"}, {Name: "authentication", Status: "pending"}, {Name: "project", Status: "pending"}}, observedSensitive
	}
	if settings.APIURL == "" {
		return []diagnosticCheck{{Name: "configuration", Status: "ok"}, {Name: "api", Status: "pending"}, {Name: "authentication", Status: "pending"}, {Name: "project", Status: "pending"}}, observedSensitive
	}
	client, err := taiga.NewClient(settings.APIURL, taiga.WithHTTPClient(a.HTTPClient), taiga.WithToken(settings.Token), taiga.WithMaxRetries(1))
	if err != nil {
		return []diagnosticCheck{{Name: "configuration", Status: "error", Code: "invalid_api_url"}}, observedSensitive
	}
	checks = append(checks, diagnosticCheck{Name: "configuration", Status: "ok"})
	started := time.Now()
	var locales []map[string]any
	if _, err := client.Get(ctx, "locales", nil, &locales); err != nil {
		checks = append(checks, diagnosticFailure("api", err))
		checks = append(checks, diagnosticCheck{Name: "authentication", Status: "pending"}, diagnosticCheck{Name: "project", Status: "pending"})
		return checks, observedSensitive
	}
	checks = append(checks, diagnosticCheck{Name: "api", Status: "ok", LatencyMS: max(time.Since(started).Round(time.Millisecond).Milliseconds(), 1)})
	if settings.Token == "" {
		checks = append(checks, diagnosticCheck{Name: "authentication", Status: "pending"}, diagnosticCheck{Name: "project", Status: "pending"})
		return checks, observedSensitive
	}
	user, err := client.Me(ctx)
	if err != nil {
		checks = append(checks, diagnosticFailure("authentication", err), diagnosticCheck{Name: "project", Status: "pending"})
		return checks, observedSensitive
	}
	observedSensitive = append(observedSensitive, user.Username, user.FullName, user.Email)
	checks = append(checks, diagnosticCheck{Name: "authentication", Status: "ok"})
	if settings.Project == "" {
		checks = append(checks, diagnosticCheck{Name: "project", Status: "pending"})
		return checks, observedSensitive
	}
	project, err := client.GetProjectBySlug(ctx, settings.Project)
	if err != nil {
		checks = append(checks, diagnosticFailure("project", err))
	} else {
		observedSensitive = append(observedSensitive, project.Name, project.Slug, project.Description)
		checks = append(checks, diagnosticCheck{Name: "project", Status: "ok"})
	}
	return checks, observedSensitive
}

func diagnosticFailure(name string, err error) diagnosticCheck {
	_, body := classifyError(err)
	return diagnosticCheck{Name: name, Status: "error", Code: body.Code, Retryable: body.Retryable, UpstreamStatus: body.UpstreamStatus}
}

func mustDiagnosticJSON(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func encodeDiagnosticZip(files map[string][]byte, stamp time.Time) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range sortedDiagnosticNames(files) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp}
		header.SetMode(0o600)
		part, err := writer.CreateHeader(header)
		if err == nil {
			_, err = part.Write(files[name])
		}
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeDiagnosticBundle(path string, data []byte, force bool) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".taiga-diagnostics-*.zip")
	if err != nil {
		return fmt.Errorf("create diagnostic bundle: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if !force {
		if err := os.Link(temporaryPath, path); err != nil {
			if _, statErr := os.Stat(path); statErr == nil {
				return validationError("output_exists", "diagnostic bundle output already exists; pass --force to replace it")
			}
			return fmt.Errorf("write diagnostic bundle without overwrite: %w", err)
		}
		return nil
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	backupFile, err := os.CreateTemp(directory, ".taiga-diagnostics-backup-*.zip")
	if err != nil {
		return fmt.Errorf("prepare diagnostic bundle replacement: %w", err)
	}
	backupPath := backupFile.Name()
	_ = backupFile.Close()
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("backup previous diagnostic bundle: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		return fmt.Errorf("replace diagnostic bundle: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove previous diagnostic bundle backup: %w", err)
	}
	return nil
}

func sortedDiagnosticNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// minimumSensitiveLength bounds the redaction tripwire away from values so
// short that they collide with the bundle's own schema vocabulary. Below four
// characters a profile named "api" or "ok" would match the fixed check names
// and refuse every bundle, so such values are left to the schema itself, which
// never copies runtime identifiers into a report.
const minimumSensitiveLength = 4

// redactionExemptFiles holds bundle members built purely from compile-time
// constants. manifest.json embeds an English privacy notice mentioning
// "usernames", "paths" and "logs", which would otherwise match legitimate
// short profile and project names.
var redactionExemptFiles = map[string]struct{}{"manifest.json": {}}

// containsSensitiveValue reports the first runtime identifier that reached the
// bundle, scanning the plaintext members rather than the compressed archive so
// that Deflate cannot hide a leak from the check.
func containsSensitiveValue(files map[string][]byte, values []string) string {
	for _, name := range sortedDiagnosticNames(files) {
		if _, exempt := redactionExemptFiles[name]; exempt {
			continue
		}
		content := strings.ToLower(string(files[name]))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if len(value) < minimumSensitiveLength {
				continue
			}
			if strings.Contains(content, strings.ToLower(value)) {
				return value
			}
		}
	}
	return ""
}
