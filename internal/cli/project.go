package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/config"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) projectCommand() *cobra.Command {
	command := &cobra.Command{Use: "project", Short: "Work with Taiga projects"}
	command.AddCommand(
		a.projectListCommand(), a.projectViewCommand(), a.projectUseCommand(), a.projectCreateCommand(),
		a.projectEditCommand(), a.projectExportCommand(), a.projectImportCommand(),
		a.projectArchiveCommand(true), a.projectArchiveCommand(false), a.projectDeleteCommand(),
		a.projectLikeCommand(true), a.projectLikeCommand(false), a.projectFansCommand(), a.projectTransferCommand(),
	)
	return command
}

func (a *App) projectCreateCommand() *cobra.Command {
	var name, description, template string
	var public, dryRun bool
	command := &cobra.Command{
		Use: "create", Short: "Create a project from a template",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return usageError("--name is required")
			}
			if strings.TrimSpace(template) == "" {
				return usageError("--template is required")
			}
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			selected, err := a.resolveProjectTemplate(cmd.Context(), client, template)
			if err != nil {
				return err
			}
			request := taiga.CreateProjectRequest{Name: name, Description: description, CreationTemplate: selected.ID, IsPrivate: !public}
			if dryRun {
				return a.renderDryRun("create project", name, map[string]any{"name": name, "description": description, "template": selected.Slug, "template_id": selected.ID, "private": !public})
			}
			project, err := client.CreateProject(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderProjectMutation("Created", project)
		},
	}
	command.Flags().StringVar(&name, "name", "", "project name")
	command.Flags().StringVar(&description, "description", "", "project description")
	command.Flags().StringVar(&template, "template", "", "project template ID, slug, or name")
	command.Flags().BoolVar(&public, "public", false, "create a public project; private by default")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	_ = command.RegisterFlagCompletionFunc("template", a.completeProjectTemplates)
	return command
}

func (a *App) projectEditCommand() *cobra.Command {
	var name, description string
	var public, private bool
	var epics, backlog, kanban, wiki, issues bool
	var dryRun bool
	command := &cobra.Command{
		Use: "edit <slug>", Short: "Edit a project", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if public && private {
				return usageError("--public and --private cannot be used together")
			}
			changed := cmd.Flags().Changed
			if !changed("name") && !changed("description") && !changed("public") && !changed("private") && !changed("epics") && !changed("backlog") && !changed("kanban") && !changed("wiki") && !changed("issues") {
				return usageError("at least one edit flag is required")
			}
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			project, err := client.GetProjectBySlug(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			request := taiga.UpdateProjectRequest{}
			if changed("name") {
				if strings.TrimSpace(name) == "" {
					return usageError("--name cannot be empty")
				}
				request.Name = &name
			}
			if changed("description") {
				request.Description = &description
			}
			if public || private {
				value := private
				request.IsPrivate = &value
			}
			if changed("epics") {
				request.IsEpicsActivated = &epics
			}
			if changed("backlog") {
				request.IsBacklogActivated = &backlog
			}
			if changed("kanban") {
				request.IsKanbanActivated = &kanban
			}
			if changed("wiki") {
				request.IsWikiActivated = &wiki
			}
			if changed("issues") {
				request.IsIssuesActivated = &issues
			}
			if dryRun {
				return a.renderDryRun("edit project", project.Slug, map[string]any{"name": request.Name, "description": request.Description, "private": request.IsPrivate, "epics": request.IsEpicsActivated, "backlog": request.IsBacklogActivated, "kanban": request.IsKanbanActivated, "wiki": request.IsWikiActivated, "issues": request.IsIssuesActivated})
			}
			updated, err := client.UpdateProject(cmd.Context(), project.ID, request)
			if err != nil {
				return err
			}
			return a.renderProjectMutation("Updated", updated)
		},
	}
	command.Flags().StringVar(&name, "name", "", "new project name")
	command.Flags().StringVar(&description, "description", "", "new project description")
	command.Flags().BoolVar(&public, "public", false, "make the project public")
	command.Flags().BoolVar(&private, "private", false, "make the project private")
	command.Flags().BoolVar(&epics, "epics", false, "enable or disable Epics")
	command.Flags().BoolVar(&backlog, "backlog", false, "enable or disable Scrum backlog")
	command.Flags().BoolVar(&kanban, "kanban", false, "enable or disable Kanban")
	command.Flags().BoolVar(&wiki, "wiki", false, "enable or disable Wiki")
	command.Flags().BoolVar(&issues, "issues", false, "enable or disable Issues")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) projectArchiveCommand(archive bool) *cobra.Command {
	name := "unarchive"
	if archive {
		name = "archive"
	}
	command := &cobra.Command{
		Use: name + " <slug>", Short: name + " a project when the server supports it", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			if _, err := client.GetProjectBySlug(cmd.Context(), args[0]); err != nil {
				return err
			}
			return validationError("unsupported_capability", "Taiga 6 exposes project archived_code as read-only and has no REST archive/unarchive action; a site administrator must manage it")
		},
	}
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) projectDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{
		Use: "delete <slug>", Short: "Request permanent project deletion", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			project, err := client.GetProjectBySlug(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun("delete project", project.Slug, map[string]any{"name": project.Name, "permanent": true, "asynchronous": true})
			}
			if !yes {
				if a.global.NoInput || !a.stdinTTY() {
					return confirmationRequired("project deletion requires --yes in non-interactive mode")
				}
				answer, err := a.readLine(fmt.Sprintf("Type %s to permanently delete the project: ", project.Slug))
				if err != nil {
					return err
				}
				if answer != project.Slug {
					return confirmationRequired("project deletion was not confirmed")
				}
			}
			if err := client.DeleteProject(cmd.Context(), project.ID); err != nil {
				return err
			}
			result := map[string]any{"id": project.ID, "slug": project.Slug, "name": project.Name, "deletion_requested": true, "asynchronous": true}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Requested permanent deletion of project %s (%s)\n", project.Name, project.Slug)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "confirm permanent project deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) resolveProjectTemplate(ctx context.Context, client *taiga.Client, value string) (taiga.ProjectTemplate, error) {
	templates, err := client.ListProjectTemplates(ctx)
	if err != nil {
		return taiga.ProjectTemplate{}, err
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		for _, template := range templates {
			if template.ID == id {
				return template, nil
			}
		}
	}
	matches := []taiga.ProjectTemplate{}
	for _, template := range templates {
		if strings.EqualFold(strings.TrimSpace(value), template.Slug) || strings.EqualFold(strings.TrimSpace(value), template.Name) {
			matches = append(matches, template)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.ProjectTemplate{}, validationError("unknown_template", fmt.Sprintf("project template %q was not found", value))
	}
	return taiga.ProjectTemplate{}, validationError("ambiguous_template", fmt.Sprintf("project template %q matches multiple values", value))
}

func (a *App) renderProjectMutation(verb string, project taiga.Project) error {
	if a.global.JSON {
		return a.renderer().Data(project)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s project %s (%s)\n", verb, project.Name, project.Slug)
	}
	return nil
}

func (a *App) projectListCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 1000 || page < 1 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			projects, pagination, err := client.ListProjects(cmd.Context(), page, limit)
			if err != nil {
				return err
			}
			if a.global.JSON {
				return a.renderer().List(projects, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "SLUG\tNAME\tPRIVATE\tARCHIVED")
			for _, project := range projects {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%t\t%t\n", project.Slug, project.Name, project.IsPrivate, project.IsArchived)
			}
			return a.flushTable(writer, len(projects), pagination.Total)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum projects to return")
	return command
}

func (a *App) projectViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "view <slug>",
		Short: "View a project",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			project, err := client.GetProjectBySlug(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.global.JSON {
				return a.renderer().Data(project)
			}
			_, _ = fmt.Fprintf(a.Out, "%s\nSlug: %s\nPrivate: %t\nArchived: %t\n\n%s\n", project.Name, project.Slug, project.IsPrivate, project.IsArchived, project.Description)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) projectUseCommand() *cobra.Command {
	var local bool
	command := &cobra.Command{
		Use:   "use <slug>",
		Short: "Select a default project",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, settings, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			project, err := client.GetProjectBySlug(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if local {
				if err := a.GitLocal.Set(cmd.Context(), "profile", settings.Profile); err != nil {
					if err == config.ErrNotGitRepository {
						return validationError("not_git_repository", err.Error())
					}
					return err
				}
				if err := a.GitLocal.Set(cmd.Context(), "project", project.Slug); err != nil {
					return err
				}
			} else {
				cfg, err := a.Config.Load()
				if err != nil {
					return err
				}
				updateProfile(&cfg, settings.Profile, func(profile *config.Profile) { profile.Project = project.Slug })
				if err := a.Config.Save(cfg); err != nil {
					return err
				}
			}
			result := map[string]any{"profile": settings.Profile, "project": project, "local": local}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Using project %s (%s)%s\n", project.Name, project.Slug, map[bool]string{true: " for this Git repository", false: ""}[local])
			}
			return nil
		},
	}
	command.Flags().BoolVar(&local, "local", false, "save profile/project mapping in .git/config")
	command.ValidArgsFunction = a.completeProjects
	return command
}
