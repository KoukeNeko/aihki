package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) wikiLinkCommand() *cobra.Command {
	command := &cobra.Command{Use: "wiki-link", Aliases: []string{"wikilink"}, Short: "Manage project Wiki navigation links"}
	command.AddCommand(a.wikiLinkListCommand(), a.wikiLinkViewCommand(), a.wikiLinkCreateCommand(), a.wikiLinkEditCommand(), a.wikiLinkDeleteCommand())
	return command
}

func (a *App) wikiLinkListCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{Use: "list", Short: "List Wiki links in the selected project", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		if page < 1 || limit < 1 || limit > 1000 {
			return usageError("--page must be positive and --limit must be between 1 and 1000")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		links, pagination, err := client.ListWikiLinks(cmd.Context(), project.ID, page, limit)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(links, pagination)
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tHREF\tTITLE\tORDER")
		for _, link := range links {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%d\n", link.ID, link.Href, link.Title, link.Order)
		}
		return a.flushTable(writer, len(links), pagination.Total)
	}}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum Wiki links to return")
	return command
}

func (a *App) wikiLinkViewCommand() *cobra.Command {
	return &cobra.Command{Use: "view <id|href|title>", Short: "View a Wiki link", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, _, link, err := a.resolveWikiLink(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(link)
		}
		_, _ = fmt.Fprintf(a.Out, "%s\nID: %d\nHref: %s\nOrder: %d\n", link.Title, link.ID, link.Href, link.Order)
		return nil
	}}
}

func (a *App) wikiLinkCreateCommand() *cobra.Command {
	var title string
	var order int64
	var dryRun bool
	command := &cobra.Command{Use: "create", Short: "Create a Wiki link and its page when permitted", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(title) == "" {
			return usageError("--title is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		request := taiga.CreateWikiLinkRequest{Project: project.ID, Title: title}
		if cmd.Flags().Changed("order") {
			request.Order = &order
		}
		if dryRun {
			return a.renderDryRun("create Wiki link", project.Slug, map[string]any{"title": title, "href": "server-generated-from-title", "order": request.Order, "may_create_page": true})
		}
		link, err := client.CreateWikiLink(cmd.Context(), request)
		if err != nil {
			return err
		}
		return a.renderWikiLinkMutation("Created", project.Slug, link)
	}}
	command.Flags().StringVar(&title, "title", "", "navigation title")
	command.Flags().Int64Var(&order, "order", 0, "navigation order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	return command
}

func (a *App) wikiLinkEditCommand() *cobra.Command {
	var title string
	var order int64
	var dryRun bool
	command := &cobra.Command{Use: "edit <id|href|title>", Short: "Edit a Wiki link title or order", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("title") && !cmd.Flags().Changed("order") {
			return usageError("--title or --order is required")
		}
		client, project, link, err := a.resolveWikiLink(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		request := taiga.UpdateWikiLinkRequest{}
		if cmd.Flags().Changed("title") {
			if strings.TrimSpace(title) == "" {
				return usageError("--title cannot be empty")
			}
			request.Title = &title
		}
		if cmd.Flags().Changed("order") {
			request.Order = &order
		}
		if dryRun {
			return a.renderDryRun("edit Wiki link", project.Slug+"#"+link.Href, map[string]any{"title": request.Title, "order": request.Order})
		}
		updated, err := client.UpdateWikiLink(cmd.Context(), link.ID, request)
		if err != nil {
			return err
		}
		return a.renderWikiLinkMutation("Updated", project.Slug, updated)
	}}
	command.Flags().StringVar(&title, "title", "", "new navigation title")
	command.Flags().Int64Var(&order, "order", 0, "new navigation order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	return command
}

func (a *App) wikiLinkDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <id|href|title>", Short: "Delete a Wiki link without deleting its page", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, link, err := a.resolveWikiLink(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete Wiki link", project.Slug+"#"+link.Href, map[string]any{"id": link.ID, "title": link.Title, "page_deleted": false})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("Wiki link deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to delete the Wiki link: ", link.Href))
			if err != nil {
				return err
			}
			if answer != link.Href {
				return confirmationRequired("Wiki link deletion was not confirmed")
			}
		}
		if err := client.DeleteWikiLink(cmd.Context(), link.ID); err != nil {
			return err
		}
		result := map[string]any{"id": link.ID, "project": project.Slug, "href": link.Href, "title": link.Title, "deleted": true, "verified": true, "page_deleted": false}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		if !a.global.Quiet {
			_, _ = fmt.Fprintf(a.Out, "Deleted Wiki link %s#%s; page retained\n", project.Slug, link.Href)
		}
		return nil
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm Wiki link deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	return command
}

func (a *App) resolveWikiLink(ctx context.Context, value string) (*taiga.Client, taiga.Project, taiga.WikiLink, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.WikiLink{}, err
	}
	links, _, err := client.ListWikiLinks(ctx, project.ID, 1, 1000)
	if err != nil {
		return nil, taiga.Project{}, taiga.WikiLink{}, err
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		for _, link := range links {
			if link.ID == id {
				return client, project, link, nil
			}
		}
	}
	matches := []taiga.WikiLink{}
	for _, link := range links {
		if strings.EqualFold(strings.TrimSpace(value), link.Href) || strings.EqualFold(strings.TrimSpace(value), link.Title) {
			matches = append(matches, link)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	if len(matches) == 1 {
		return client, project, matches[0], nil
	}
	if len(matches) == 0 {
		return nil, taiga.Project{}, taiga.WikiLink{}, validationError("unknown_wiki_link", fmt.Sprintf("Wiki link %q was not found", value))
	}
	return nil, taiga.Project{}, taiga.WikiLink{}, validationError("ambiguous_wiki_link", fmt.Sprintf("Wiki link %q matches multiple links", value))
}

func (a *App) renderWikiLinkMutation(verb, project string, link taiga.WikiLink) error {
	if a.global.JSON {
		return a.renderer().Data(link)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s Wiki link %s#%s: %s\n", verb, project, link.Href, link.Title)
	}
	return nil
}
