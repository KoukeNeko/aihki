package cli

import (
	"fmt"
	"time"

	"github.com/KoukeNeko/taiga-cli/internal/credential"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (a *App) doctorCommand() *cobra.Command {
	var siteURL string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose a Taiga endpoint and current context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if siteURL != "" && a.global.APIURL != "" {
				return usageError("--url and --api-url are mutually exclusive")
			}
			settings, _, err := a.resolveSettings(ctx)
			if err != nil {
				return err
			}
			apiURL := a.global.APIURL
			checks := []doctorCheck{}
			if siteURL != "" {
				front, err := taiga.DiscoverAPI(ctx, a.HTTPClient, siteURL)
				if err != nil {
					return err
				}
				apiURL = front.API
				checks = append(checks, doctorCheck{Name: "frontend", Status: "ok", Detail: firstNonEmpty(front.Site, "API found directly")})
			}
			if apiURL == "" {
				apiURL = settings.APIURL
			}
			if apiURL == "" {
				return validationError("missing_api_url", "provide --url or --api-url, or configure a profile")
			}
			token := ""
			if settings.APIURL == apiURL {
				token = settings.Token
			}
			options := []taiga.ClientOption{taiga.WithHTTPClient(a.HTTPClient), taiga.WithToken(token)}
			if token != "" && settings.RefreshToken != "" && a.Credentials != nil {
				account := credential.Account(settings.Profile, settings.APIURL)
				options = append(options, taiga.WithRefreshToken(settings.RefreshToken, func(authToken, refreshToken string) error {
					return a.Credentials.Set(account, credential.Tokens{AuthToken: authToken, RefreshToken: refreshToken})
				}))
			}
			if a.global.Verbose {
				options = append(options, taiga.WithVerbose(a.Err))
			}
			client, err := taiga.NewClient(apiURL, options...)
			if err != nil {
				return err
			}
			started := time.Now()
			var locales []map[string]any
			if _, err := client.Get(ctx, "locales", nil, &locales); err != nil {
				return err
			}
			checks = append(checks, doctorCheck{Name: "api", Status: "ok", Detail: fmt.Sprintf("%s (%s)", client.APIURL(), time.Since(started).Round(time.Millisecond))})
			if token == "" {
				checks = append(checks, doctorCheck{Name: "authentication", Status: "pending", Detail: "no credential available"})
			} else {
				user, err := client.Me(ctx)
				if err != nil {
					return err
				}
				checks = append(checks, doctorCheck{Name: "authentication", Status: "ok", Detail: user.Username})
			}
			if settings.Project != "" && token != "" {
				project, err := client.GetProjectBySlug(ctx, settings.Project)
				if err != nil {
					return err
				}
				checks = append(checks, doctorCheck{Name: "project", Status: "ok", Detail: project.Slug})
			} else {
				checks = append(checks, doctorCheck{Name: "project", Status: "pending", Detail: "no default project selected"})
			}
			result := map[string]any{"profile": settings.Profile, "api_url": apiURL, "checks": checks}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			for _, check := range checks {
				_, _ = fmt.Fprintf(a.Out, "%-16s %-8s %s\n", check.Name, check.Status, check.Detail)
			}
			return nil
		},
	}
	addSiteURLFlag(command, &siteURL)
	command.AddCommand(a.doctorBundleCommand())
	return command
}
