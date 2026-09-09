package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type webhookView struct {
	ID          int64  `json:"id"`
	Project     string `json:"project"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	LogsCounter int    `json:"logs_counter"`
}

func (a *App) webhookCommand() *cobra.Command {
	command := &cobra.Command{Use: "webhook", Short: "Manage project webhooks"}
	command.AddCommand(a.webhookListCommand(), a.webhookViewCommand(), a.webhookCreateCommand(), a.webhookEditCommand(), a.webhookTestCommand(), a.webhookDeleteCommand())
	return command
}

func (a *App) webhookListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List project webhooks", RunE: func(cmd *cobra.Command, _ []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		hooks, err := client.ListWebhooks(cmd.Context(), project.ID)
		if err != nil {
			return err
		}
		views := make([]webhookView, 0, len(hooks))
		for _, hook := range hooks {
			views = append(views, makeWebhookView(hook, project.Slug))
		}
		if a.global.JSON {
			return a.renderer().List(views, map[string]any{"total": len(views)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tNAME\tURL\tLOGS")
		for _, hook := range views {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%d\n", hook.ID, hook.Name, hook.URL, hook.LogsCounter)
		}
		return writer.Flush()
	}}
}

func (a *App) webhookViewCommand() *cobra.Command {
	return &cobra.Command{Use: "view <id|name>", Short: "View webhook metadata without its secret", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, project, hook, err := a.loadWebhook(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		view := makeWebhookView(hook, project.Slug)
		if a.global.JSON {
			return a.renderer().Data(view)
		}
		_, _ = fmt.Fprintf(a.Out, "%s\nID:      %d\nProject: %s\nURL:     %s\nLogs:    %d\n", view.Name, view.ID, view.Project, view.URL, view.LogsCounter)
		return nil
	}}
}

func (a *App) webhookCreateCommand() *cobra.Command {
	var name, rawURL, secret string
	var dryRun bool
	command := &cobra.Command{Use: "create", Short: "Create a project webhook", RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(rawURL) == "" || secret == "" {
			return usageError("--name, --url, and --secret are required")
		}
		if err := validateWebhookURL(rawURL); err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("create webhook", project.Slug, map[string]any{"name": name, "url": rawURL, "secret_set": true})
		}
		hook, err := client.CreateWebhook(cmd.Context(), taiga.CreateWebhookRequest{Project: project.ID, Name: name, URL: rawURL, Key: secret})
		if err != nil {
			return err
		}
		return a.renderWebhookMutation("Created", makeWebhookView(hook, project.Slug))
	}}
	command.Flags().StringVar(&name, "name", "", "webhook name")
	command.Flags().StringVar(&rawURL, "url", "", "HTTPS or HTTP delivery URL")
	command.Flags().StringVar(&secret, "secret", "", "webhook signing secret; never printed")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	return command
}

func (a *App) webhookEditCommand() *cobra.Command {
	var name, rawURL, secret string
	var dryRun bool
	command := &cobra.Command{Use: "edit <id|name>", Short: "Edit a project webhook", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		changed := cmd.Flags().Changed
		if !changed("name") && !changed("url") && !changed("secret") {
			return usageError("--name, --url, or --secret is required")
		}
		client, project, hook, err := a.loadWebhook(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		request := taiga.UpdateWebhookRequest{}
		if changed("name") {
			if strings.TrimSpace(name) == "" {
				return usageError("--name cannot be empty")
			}
			request.Name = &name
		}
		if changed("url") {
			if err := validateWebhookURL(rawURL); err != nil {
				return err
			}
			request.URL = &rawURL
		}
		if changed("secret") {
			if secret == "" {
				return usageError("--secret cannot be empty")
			}
			request.Key = &secret
		}
		if dryRun {
			return a.renderDryRun("edit webhook", strconv.FormatInt(hook.ID, 10), map[string]any{"name": request.Name, "url": request.URL, "secret_set": request.Key != nil})
		}
		updated, err := client.UpdateWebhook(cmd.Context(), hook.ID, request)
		if err != nil {
			return err
		}
		return a.renderWebhookMutation("Updated", makeWebhookView(updated, project.Slug))
	}}
	command.Flags().StringVar(&name, "name", "", "new webhook name")
	command.Flags().StringVar(&rawURL, "url", "", "new delivery URL")
	command.Flags().StringVar(&secret, "secret", "", "new signing secret; never printed")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	return command
}

func (a *App) webhookTestCommand() *cobra.Command {
	return &cobra.Command{Use: "test <id|name>", Short: "Ask Taiga to send a test webhook", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, _, hook, err := a.loadWebhook(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		log, err := client.TestWebhook(cmd.Context(), hook.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(log)
		}
		_, _ = fmt.Fprintf(a.Out, "Webhook test status %d in %.3fs\n", log.Status, log.Duration)
		return nil
	}}
}

func (a *App) webhookDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <id|name>", Short: "Delete a project webhook", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, hook, err := a.loadWebhook(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete webhook", strconv.FormatInt(hook.ID, 10), map[string]any{"name": hook.Name, "url": hook.URL})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("webhook deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to delete the webhook: ", hook.Name))
			if err != nil {
				return err
			}
			if answer != hook.Name {
				return confirmationRequired("webhook deletion was not confirmed")
			}
		}
		if err := client.DeleteWebhook(cmd.Context(), hook.ID); err != nil {
			return err
		}
		result := map[string]any{"id": hook.ID, "project": project.Slug, "name": hook.Name, "deleted": true}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		_, _ = fmt.Fprintf(a.Out, "Deleted webhook %s\n", hook.Name)
		return nil
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm webhook deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	return command
}

func (a *App) loadWebhook(ctx context.Context, value string) (*taiga.Client, taiga.Project, taiga.Webhook, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.Webhook{}, err
	}
	hooks, err := client.ListWebhooks(ctx, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.Webhook{}, err
	}
	id, idErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	matches := []taiga.Webhook{}
	for _, hook := range hooks {
		if (idErr == nil && hook.ID == id) || strings.EqualFold(value, hook.Name) {
			matches = append(matches, hook)
		}
	}
	if len(matches) == 1 {
		return client, project, matches[0], nil
	}
	if len(matches) == 0 {
		return nil, taiga.Project{}, taiga.Webhook{}, validationError("unknown_webhook", fmt.Sprintf("project webhook %q was not found", value))
	}
	return nil, taiga.Project{}, taiga.Webhook{}, validationError("ambiguous_webhook", fmt.Sprintf("webhook name %q matches multiple entries; use ID", value))
}

func validateWebhookURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return usageError("--url must be an HTTP(S) URL without embedded credentials")
	}
	return nil
}

func makeWebhookView(hook taiga.Webhook, project string) webhookView {
	return webhookView{ID: hook.ID, Project: project, Name: hook.Name, URL: hook.URL, LogsCounter: hook.LogsCounter}
}

func (a *App) renderWebhookMutation(verb string, hook webhookView) error {
	if a.global.JSON {
		return a.renderer().Data(hook)
	}
	_, _ = fmt.Fprintf(a.Out, "%s webhook %s (%d)\n", verb, hook.Name, hook.ID)
	return nil
}
