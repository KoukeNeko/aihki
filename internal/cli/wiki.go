package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

type wikiView struct {
	ID           int64  `json:"id"`
	Project      string `json:"project"`
	Slug         string `json:"slug"`
	Content      string `json:"content,omitempty"`
	HTML         string `json:"html,omitempty"`
	Owner        *int64 `json:"owner,omitempty"`
	LastModifier *int64 `json:"last_modifier,omitempty"`
	Editions     int    `json:"editions"`
	Version      int    `json:"version"`
	IsWatcher    bool   `json:"is_watcher"`
	CreatedDate  string `json:"created_date,omitempty"`
	ModifiedDate string `json:"modified_date,omitempty"`
}

type wikiTarget struct {
	Client  *taiga.Client
	Project taiga.Project
	Page    taiga.WikiPage
}

func (a *App) wikiCommand() *cobra.Command {
	command := &cobra.Command{Use: "wiki", Short: "Work with Taiga wiki pages"}
	command.AddCommand(
		a.wikiListCommand(), a.wikiViewCommand(), a.wikiCreateCommand(), a.wikiEditCommand(), a.wikiDeleteCommand(),
		a.watchCommand("wiki", true), a.watchCommand("wiki", false), a.historyCommand("wiki"),
		a.participantCommand("wiki", "watchers"),
	)
	return command
}

func (a *App) wikiListCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use: "list", Short: "List pages in the selected project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if page < 1 || limit < 1 || limit > 1000 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			pages, pagination, err := client.ListWikiPages(cmd.Context(), project.ID, page, limit)
			if err != nil {
				return err
			}
			views := make([]wikiView, 0, len(pages))
			for _, item := range pages {
				views = append(views, makeWikiView(item, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "SLUG\tEDITIONS\tVERSION\tMODIFIED")
			for _, item := range views {
				_, _ = fmt.Fprintf(writer, "%s\t%d\t%d\t%s\n", item.Slug, item.Editions, item.Version, item.ModifiedDate)
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum wiki pages to return")
	return command
}

func (a *App) wikiViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "view <slug|project#slug|url>", Short: "View a wiki page", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadWikiTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			view := makeWikiView(target.Page, target.Project.Slug)
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "%s\nProject:  %s\nVersion:  %d\nEditions: %d\nModified: %s\n\n%s\n", view.Slug, view.Project, view.Version, view.Editions, view.ModifiedDate, view.Content)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeWikiPages
	return command
}

func (a *App) wikiCreateCommand() *cobra.Command {
	var slug, body, bodyFile string
	var dryRun bool
	command := &cobra.Command{
		Use: "create", Short: "Create a wiki page",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(slug) == "" {
				return usageError("--slug is required")
			}
			if cmd.Flags().Changed("body") && cmd.Flags().Changed("body-file") {
				return usageError("--body and --body-file cannot be used together")
			}
			content, err := readWikiContent(a.In, body, bodyFile)
			if err != nil {
				return err
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			request := taiga.CreateWikiPageRequest{Project: project.ID, Slug: strings.TrimSpace(slug), Content: content}
			if dryRun {
				return a.renderDryRun("create wiki page", project.Slug+"#"+request.Slug, map[string]any{"slug": request.Slug, "content": request.Content})
			}
			created, err := client.CreateWikiPage(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderWikiMutation("Created", makeWikiView(created, project.Slug))
		},
	}
	command.Flags().StringVar(&slug, "slug", "", "wiki page slug")
	command.Flags().StringVar(&body, "body", "", "wiki page content")
	command.Flags().StringVar(&bodyFile, "body-file", "", "read content from a file, or - for stdin")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	return command
}

func (a *App) wikiEditCommand() *cobra.Command {
	var slug, body, bodyFile string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use: "edit <slug|project#slug|url>", Short: "Edit a wiki page with OCC", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("slug") && !cmd.Flags().Changed("body") && !cmd.Flags().Changed("body-file") {
				return usageError("--slug, --body, or --body-file is required")
			}
			if cmd.Flags().Changed("body") && cmd.Flags().Changed("body-file") {
				return usageError("--body and --body-file cannot be used together")
			}
			target, err := a.loadWikiTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			version := target.Page.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			request := taiga.UpdateWikiPageRequest{Version: version}
			if cmd.Flags().Changed("slug") {
				value := strings.TrimSpace(slug)
				if value == "" {
					return usageError("--slug cannot be empty")
				}
				request.Slug = &value
			}
			if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
				content, err := readWikiContent(a.In, body, bodyFile)
				if err != nil {
					return err
				}
				request.Content = &content
			}
			if dryRun {
				return a.renderDryRun("edit wiki page", target.Project.Slug+"#"+target.Page.Slug, map[string]any{"base_version": version, "slug": request.Slug, "content": request.Content})
			}
			updated, err := target.Client.UpdateWikiPage(cmd.Context(), target.Page.ID, request)
			if err != nil {
				return err
			}
			return a.renderWikiMutation("Updated", makeWikiView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&slug, "slug", "", "new wiki page slug")
	command.Flags().StringVar(&body, "body", "", "new wiki page content; empty clears the page")
	command.Flags().StringVar(&bodyFile, "body-file", "", "read content from a file, or - for stdin")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeWikiPages
	return command
}

func (a *App) wikiDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{
		Use: "delete <slug|project#slug|url>", Short: "Delete a wiki page", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadWikiTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun("delete wiki page", target.Project.Slug+"#"+target.Page.Slug, map[string]any{"slug": target.Page.Slug})
			}
			if !yes {
				if a.global.NoInput || !a.stdinTTY() {
					return confirmationRequired("wiki deletion requires --yes in non-interactive mode")
				}
				answer, err := a.readLine(fmt.Sprintf("Type %s to delete the wiki page: ", target.Page.Slug))
				if err != nil {
					return err
				}
				if answer != target.Page.Slug {
					return confirmationRequired("wiki deletion was not confirmed")
				}
			}
			if err := target.Client.DeleteWikiPage(cmd.Context(), target.Page.ID); err != nil {
				return err
			}
			result := map[string]any{"id": target.Page.ID, "project": target.Project.Slug, "slug": target.Page.Slug, "deleted": true}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Deleted wiki page %s#%s\n", target.Project.Slug, target.Page.Slug)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "confirm wiki page deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	command.ValidArgsFunction = a.completeWikiPages
	return command
}

func (a *App) selectedProject(ctx context.Context) (*taiga.Client, taiga.Project, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return nil, taiga.Project{}, err
	}
	if settings.Project == "" {
		return nil, taiga.Project{}, validationError("missing_project", "no project selected; run `aihki project use <slug>` or pass --project")
	}
	project, err := client.GetProjectBySlug(ctx, settings.Project)
	return client, project, err
}

func (a *App) loadWikiTarget(ctx context.Context, value string) (wikiTarget, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return wikiTarget{}, err
	}
	ref, err := taiga.ParseWikiRef(value, settings.Project)
	if err != nil {
		return wikiTarget{}, validationError("invalid_wiki_ref", err.Error())
	}
	project, err := client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return wikiTarget{}, err
	}
	page, err := client.GetWikiPageBySlug(ctx, project.ID, ref.Slug)
	if err != nil {
		return wikiTarget{}, err
	}
	return wikiTarget{Client: client, Project: project, Page: page}, nil
}

func readWikiContent(input io.Reader, body, bodyFile string) (string, error) {
	if bodyFile == "" {
		return body, nil
	}
	if body != "" {
		return "", usageError("--body and --body-file cannot be used together")
	}
	var data []byte
	var err error
	if bodyFile == "-" {
		data, err = io.ReadAll(io.LimitReader(input, maxBodyBytes))
	} else {
		data, err = os.ReadFile(bodyFile)
	}
	if err != nil {
		return "", fmt.Errorf("read wiki content: %w", err)
	}
	return string(data), nil
}

func makeWikiView(page taiga.WikiPage, projectSlug string) wikiView {
	return wikiView{
		ID: page.ID, Project: projectSlug, Slug: page.Slug, Content: page.Content, HTML: page.HTML,
		Owner: page.Owner, LastModifier: page.LastModifier, Editions: page.Editions, Version: page.Version,
		IsWatcher: page.IsWatcher, CreatedDate: page.CreatedDate, ModifiedDate: page.ModifiedDate,
	}
}

func (a *App) renderWikiMutation(verb string, view wikiView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s wiki page %s#%s (version %d)\n", verb, view.Project, view.Slug, view.Version)
	}
	return nil
}
