package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type epicView struct {
	ID                int64  `json:"id"`
	Ref               int    `json:"ref"`
	Project           string `json:"project"`
	Subject           string `json:"subject"`
	Description       string `json:"description,omitempty"`
	Color             string `json:"color,omitempty"`
	Version           int    `json:"version"`
	Status            string `json:"status"`
	Assignee          string `json:"assignee,omitempty"`
	ClientRequirement bool   `json:"client_requirement"`
	TeamRequirement   bool   `json:"team_requirement"`
	IsClosed          bool   `json:"is_closed"`
	IsBlocked         bool   `json:"is_blocked"`
	IsWatcher         bool   `json:"is_watcher"`
	CreatedDate       string `json:"created_date,omitempty"`
	ModifiedDate      string `json:"modified_date,omitempty"`
}

type epicTarget struct {
	Client  *taiga.Client
	Project taiga.Project
	Epic    taiga.Epic
	Ref     taiga.ItemRef
}

type epicStoryView struct {
	EpicID       int64  `json:"epic_id"`
	EpicRef      int    `json:"epic_ref"`
	StoryID      int64  `json:"story_id"`
	StoryRef     int    `json:"story_ref"`
	StoryProject string `json:"story_project"`
	Subject      string `json:"subject"`
	Order        int64  `json:"order"`
}

func (a *App) epicCommand() *cobra.Command {
	command := &cobra.Command{Use: "epic", Short: "Work with Taiga epics"}
	command.AddCommand(
		a.epicListCommand(), a.epicViewCommand(), a.epicCreateCommand(), a.epicEditCommand(), a.epicCloseCommand(),
		a.epicStoriesCommand(), a.epicLinkCommand(), a.epicUnlinkCommand(),
		a.deleteWorkItemCommand("epic"),
		a.watchCommand("epic", true), a.watchCommand("epic", false), a.historyCommand("epic"),
		a.voteCommand("epic", true), a.voteCommand("epic", false),
		a.participantCommand("epic", "watchers"), a.participantCommand("epic", "voters"),
	)
	return command
}

func (a *App) epicListCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{Use: "list", Short: "List epics in the selected project", RunE: func(cmd *cobra.Command, _ []string) error {
		if page < 1 || limit < 1 || limit > 1000 {
			return usageError("--page must be positive and --limit must be between 1 and 1000")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		epics, pagination, err := client.ListEpics(cmd.Context(), project.ID, page, limit)
		if err != nil {
			return err
		}
		views := make([]epicView, 0, len(epics))
		for _, epic := range epics {
			views = append(views, makeEpicView(epic, project.Slug))
		}
		if a.global.JSON {
			return a.renderer().List(views, pagination)
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "REF\tSUBJECT\tSTATUS\tASSIGNEE\tVERSION")
		for _, epic := range views {
			_, _ = fmt.Fprintf(writer, "#%d\t%s\t%s\t%s\t%d\n", epic.Ref, epic.Subject, epic.Status, epic.Assignee, epic.Version)
		}
		return a.flushTable(writer, len(views), pagination.Total)
	}}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum epics to return")
	return command
}

func (a *App) epicViewCommand() *cobra.Command {
	command := &cobra.Command{Use: "view <ref|project#ref|url>", Short: "View an epic", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		target, err := a.loadEpicTarget(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		view := makeEpicView(target.Epic, target.Project.Slug)
		if a.global.JSON {
			return a.renderer().Data(view)
		}
		_, _ = fmt.Fprintf(a.Out, "#%d  %s\nStatus:   %s\nAssignee: %s\nVersion:  %d\nColor:    %s\n\n%s\n", view.Ref, view.Subject, view.Status, view.Assignee, view.Version, view.Color, view.Description)
		return nil
	}}
	command.ValidArgsFunction = a.completeEpics
	return command
}

func (a *App) epicCreateCommand() *cobra.Command {
	var subject, description, status, assignee, color string
	var dryRun bool
	command := &cobra.Command{Use: "create", Short: "Create an epic", RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(subject) == "" {
			return usageError("--subject is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		request := taiga.CreateEpicRequest{Project: project.ID, Subject: subject, Description: description, Color: color}
		if status != "" {
			selected, err := a.resolveEpicStatus(cmd.Context(), client, project.ID, status, false)
			if err != nil {
				return err
			}
			request.Status = &selected.ID
		}
		if assignee != "" {
			id, err := a.resolveAssignee(cmd.Context(), client, project.ID, assignee)
			if err != nil {
				return err
			}
			request.AssignedTo = &id
		}
		if dryRun {
			return a.renderDryRun("create epic", project.Slug, map[string]any{"subject": subject, "description": description, "status": status, "assignee": assignee, "color": color})
		}
		created, err := client.CreateEpic(cmd.Context(), request)
		if err != nil {
			return err
		}
		return a.renderEpicMutation("Created", makeEpicView(created, project.Slug))
	}}
	command.Flags().StringVar(&subject, "subject", "", "epic subject")
	command.Flags().StringVar(&description, "description", "", "epic description")
	command.Flags().StringVar(&status, "status", "", "epic status name")
	command.Flags().StringVar(&assignee, "assignee", "", "project member username or name")
	command.Flags().StringVar(&color, "color", "", "epic color")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	_ = command.RegisterFlagCompletionFunc("status", a.completeEpicStatuses)
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	return command
}

func (a *App) epicEditCommand() *cobra.Command {
	var subject, description, status, assignee, color string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{Use: "edit <ref|project#ref|url>", Short: "Edit an epic with OCC", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("subject") && !cmd.Flags().Changed("description") && !cmd.Flags().Changed("status") && !cmd.Flags().Changed("assignee") && !cmd.Flags().Changed("color") {
			return usageError("at least one edit flag is required")
		}
		target, err := a.loadEpicTarget(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		version := target.Epic.Version
		if baseVersion > 0 {
			version = baseVersion
		}
		request := taiga.UpdateEpicRequest{Version: version}
		if cmd.Flags().Changed("subject") {
			if strings.TrimSpace(subject) == "" {
				return usageError("--subject cannot be empty")
			}
			request.Subject = &subject
		}
		if cmd.Flags().Changed("description") {
			request.Description = &description
		}
		if cmd.Flags().Changed("status") {
			selected, err := a.resolveEpicStatus(cmd.Context(), target.Client, target.Project.ID, status, false)
			if err != nil {
				return err
			}
			request.Status = &selected.ID
		}
		if cmd.Flags().Changed("assignee") {
			var value *int64
			if !strings.EqualFold(strings.TrimSpace(assignee), "none") {
				id, err := a.resolveAssignee(cmd.Context(), target.Client, target.Project.ID, assignee)
				if err != nil {
					return err
				}
				value = &id
			}
			request.AssignedTo = &value
		}
		if cmd.Flags().Changed("color") {
			request.Color = &color
		}
		if dryRun {
			return a.renderDryRun("edit epic", fmt.Sprintf("%s#%d", target.Project.Slug, target.Epic.Ref), map[string]any{"base_version": version, "subject": request.Subject, "description": request.Description, "status": status, "assignee": assignee, "color": request.Color})
		}
		updated, err := target.Client.UpdateEpic(cmd.Context(), target.Epic.ID, request)
		if err != nil {
			return err
		}
		return a.renderEpicMutation("Updated", makeEpicView(updated, target.Project.Slug))
	}}
	command.Flags().StringVar(&subject, "subject", "", "new epic subject")
	command.Flags().StringVar(&description, "description", "", "new epic description")
	command.Flags().StringVar(&status, "status", "", "new epic status name")
	command.Flags().StringVar(&assignee, "assignee", "", "member username/name, or none")
	command.Flags().StringVar(&color, "color", "", "new epic color")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	_ = command.RegisterFlagCompletionFunc("status", a.completeEpicStatuses)
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	command.ValidArgsFunction = a.completeEpics
	return command
}

func (a *App) epicCloseCommand() *cobra.Command {
	var status string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{Use: "close <ref|project#ref|url>", Short: "Move an epic to a closed status", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		target, err := a.loadEpicTarget(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		selected, err := a.resolveEpicCloseStatus(cmd.Context(), target.Client, target.Project.ID, status)
		if err != nil {
			return err
		}
		version := target.Epic.Version
		if baseVersion > 0 {
			version = baseVersion
		}
		if dryRun {
			return a.renderDryRun("close epic", fmt.Sprintf("%s#%d", target.Project.Slug, target.Epic.Ref), map[string]any{"status": selected.Name, "base_version": version})
		}
		updated, err := target.Client.UpdateEpic(cmd.Context(), target.Epic.ID, taiga.UpdateEpicRequest{Version: version, Status: &selected.ID})
		if err != nil {
			return err
		}
		return a.renderEpicMutation("Closed", makeEpicView(updated, target.Project.Slug))
	}}
	command.Flags().StringVar(&status, "status", "", "closed epic status name")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	_ = command.RegisterFlagCompletionFunc("status", a.completeEpicStatuses)
	command.ValidArgsFunction = a.completeEpics
	return command
}

func (a *App) epicStoriesCommand() *cobra.Command {
	command := &cobra.Command{Use: "stories <ref|project#ref|url>", Short: "List stories linked to an epic", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		target, err := a.loadEpicTarget(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		views, err := a.loadEpicStoryViews(cmd.Context(), target)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(views, map[string]any{"total": len(views)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "PROJECT\tREF\tSUBJECT\tORDER")
		for _, story := range views {
			_, _ = fmt.Fprintf(writer, "%s\t#%d\t%s\t%d\n", story.StoryProject, story.StoryRef, story.Subject, story.Order)
		}
		return writer.Flush()
	}}
	command.ValidArgsFunction = a.completeEpics
	return command
}

func (a *App) epicLinkCommand() *cobra.Command {
	var story string
	var dryRun bool
	command := &cobra.Command{Use: "link <ref|project#ref|url>", Short: "Link a Story to an epic", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(story) == "" {
			return usageError("--story is required")
		}
		target, linkedStory, storyProject, err := a.loadEpicAndStory(cmd.Context(), args[0], story)
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("link story to epic", fmt.Sprintf("%s#%d", target.Project.Slug, target.Epic.Ref), map[string]any{"story": fmt.Sprintf("%s#%d", storyProject.Slug, linkedStory.Ref)})
		}
		related, err := target.Client.LinkEpicUserStory(cmd.Context(), target.Epic.ID, linkedStory.ID)
		if err != nil {
			return err
		}
		view := epicStoryView{EpicID: target.Epic.ID, EpicRef: target.Epic.Ref, StoryID: linkedStory.ID, StoryRef: linkedStory.Ref, StoryProject: storyProject.Slug, Subject: linkedStory.Subject, Order: related.Order}
		if a.global.JSON {
			return a.renderer().Data(view)
		}
		_, _ = fmt.Fprintf(a.Out, "Linked %s#%d to epic %s#%d\n", storyProject.Slug, linkedStory.Ref, target.Project.Slug, target.Epic.Ref)
		return nil
	}}
	command.Flags().StringVar(&story, "story", "", "Story ref, project#ref, or URL")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the link without writing")
	command.ValidArgsFunction = a.completeEpics
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	return command
}

func (a *App) epicUnlinkCommand() *cobra.Command {
	var story string
	var dryRun bool
	command := &cobra.Command{Use: "unlink <ref|project#ref|url>", Short: "Unlink a Story from an epic", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(story) == "" {
			return usageError("--story is required")
		}
		target, linkedStory, storyProject, err := a.loadEpicAndStory(cmd.Context(), args[0], story)
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("unlink story from epic", fmt.Sprintf("%s#%d", target.Project.Slug, target.Epic.Ref), map[string]any{"story": fmt.Sprintf("%s#%d", storyProject.Slug, linkedStory.Ref)})
		}
		if err := target.Client.UnlinkEpicUserStory(cmd.Context(), target.Epic.ID, linkedStory.ID); err != nil {
			return err
		}
		result := map[string]any{"epic_ref": target.Epic.Ref, "epic_project": target.Project.Slug, "story_ref": linkedStory.Ref, "story_project": storyProject.Slug, "linked": false}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		_, _ = fmt.Fprintf(a.Out, "Unlinked %s#%d from epic %s#%d\n", storyProject.Slug, linkedStory.Ref, target.Project.Slug, target.Epic.Ref)
		return nil
	}}
	command.Flags().StringVar(&story, "story", "", "Story ref, project#ref, or URL")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the unlink without writing")
	command.ValidArgsFunction = a.completeEpics
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	return command
}

func (a *App) loadEpicTarget(ctx context.Context, value string) (epicTarget, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return epicTarget{}, err
	}
	ref, err := taiga.ParseEpicRef(value, settings.Project)
	if err != nil {
		return epicTarget{}, validationError("invalid_ref", err.Error())
	}
	project, err := client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return epicTarget{}, err
	}
	epic, err := client.GetEpicByRef(ctx, project.Slug, ref.Ref)
	if err != nil {
		return epicTarget{}, err
	}
	return epicTarget{Client: client, Project: project, Epic: epic, Ref: ref}, nil
}

func (a *App) loadEpicAndStory(ctx context.Context, epicValue, storyValue string) (epicTarget, taiga.UserStory, taiga.Project, error) {
	target, err := a.loadEpicTarget(ctx, epicValue)
	if err != nil {
		return epicTarget{}, taiga.UserStory{}, taiga.Project{}, err
	}
	ref, err := taiga.ParseStoryRef(storyValue, target.Project.Slug)
	if err != nil {
		return epicTarget{}, taiga.UserStory{}, taiga.Project{}, validationError("invalid_story_ref", err.Error())
	}
	project, err := target.Client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return epicTarget{}, taiga.UserStory{}, taiga.Project{}, err
	}
	story, err := target.Client.GetUserStoryByRef(ctx, project.Slug, ref.Ref)
	return target, story, project, err
}

func (a *App) loadEpicStoryViews(ctx context.Context, target epicTarget) ([]epicStoryView, error) {
	related, err := target.Client.ListEpicRelatedUserStories(ctx, target.Epic.ID)
	if err != nil {
		return nil, err
	}
	views := make([]epicStoryView, 0, len(related))
	projects := map[int64]taiga.Project{}
	for _, relation := range related {
		story, err := target.Client.GetUserStory(ctx, relation.UserStory)
		if err != nil {
			return nil, err
		}
		project, ok := projects[story.Project]
		if !ok {
			project, err = target.Client.GetProject(ctx, story.Project)
			if err != nil {
				return nil, err
			}
			projects[story.Project] = project
		}
		views = append(views, epicStoryView{EpicID: target.Epic.ID, EpicRef: target.Epic.Ref, StoryID: story.ID, StoryRef: story.Ref, StoryProject: project.Slug, Subject: story.Subject, Order: relation.Order})
	}
	return views, nil
}

func makeEpicView(epic taiga.Epic, projectSlug string) epicView {
	assignee := ""
	if epic.AssignedToExtraInfo != nil {
		assignee = firstNonEmpty(epic.AssignedToExtraInfo.Username, epic.AssignedToExtraInfo.FullName)
	}
	return epicView{ID: epic.ID, Ref: epic.Ref, Project: projectSlug, Subject: epic.Subject, Description: epic.Description, Color: epic.Color, Version: epic.Version, Status: epic.StatusExtraInfo.Name, Assignee: assignee, ClientRequirement: epic.ClientRequirement, TeamRequirement: epic.TeamRequirement, IsClosed: epic.IsClosed, IsBlocked: epic.IsBlocked, IsWatcher: epic.IsWatcher, CreatedDate: epic.CreatedDate, ModifiedDate: epic.ModifiedDate}
}

func (a *App) resolveEpicStatus(ctx context.Context, client *taiga.Client, projectID int64, name string, requireClosed bool) (taiga.EpicStatus, error) {
	statuses, err := client.EpicStatuses(ctx, projectID)
	if err != nil {
		return taiga.EpicStatus{}, err
	}
	matches := []taiga.EpicStatus{}
	for _, status := range statuses {
		if strings.EqualFold(strings.TrimSpace(status.Name), strings.TrimSpace(name)) && (!requireClosed || status.IsClosed) {
			matches = append(matches, status)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.EpicStatus{}, validationError("unknown_status", fmt.Sprintf("epic status %q was not found", name))
	}
	return taiga.EpicStatus{}, validationError("ambiguous_status", fmt.Sprintf("epic status %q matches multiple values", name))
}

func (a *App) resolveEpicCloseStatus(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.EpicStatus, error) {
	if name != "" {
		return a.resolveEpicStatus(ctx, client, projectID, name, true)
	}
	statuses, err := client.EpicStatuses(ctx, projectID)
	if err != nil {
		return taiga.EpicStatus{}, err
	}
	closed := []taiga.EpicStatus{}
	for _, status := range statuses {
		if status.IsClosed {
			closed = append(closed, status)
		}
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].Order < closed[j].Order })
	if len(closed) == 1 {
		return closed[0], nil
	}
	if len(closed) == 0 {
		return taiga.EpicStatus{}, validationError("missing_closed_status", "project has no closed epic status")
	}
	names := make([]string, 0, len(closed))
	for _, status := range closed {
		names = append(names, status.Name)
	}
	return taiga.EpicStatus{}, validationError("ambiguous_status", "multiple closed statuses are available; pass --status: "+strings.Join(names, ", "))
}

func (a *App) renderEpicMutation(verb string, view epicView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s epic %s#%d: %s (version %d)\n", verb, view.Project, view.Ref, view.Subject, view.Version)
	}
	return nil
}
