package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

type storyView struct {
	ID            int64            `json:"id"`
	Ref           int              `json:"ref"`
	Project       string           `json:"project"`
	Subject       string           `json:"subject"`
	Description   string           `json:"description,omitempty"`
	Version       int              `json:"version"`
	Status        string           `json:"status"`
	Sprint        string           `json:"sprint,omitempty"`
	SprintSlug    string           `json:"sprint_slug,omitempty"`
	AssignedUsers []int64          `json:"assigned_users,omitempty"`
	TotalPoints   *float64         `json:"total_points,omitempty"`
	Points        map[string]int64 `json:"points,omitempty"`
	IsClosed      bool             `json:"is_closed"`
	IsWatcher     bool             `json:"is_watcher"`
	IsBlocked     bool             `json:"is_blocked"`
	CreatedDate   string           `json:"created_date,omitempty"`
	ModifiedDate  string           `json:"modified_date,omitempty"`
}

type storyTarget struct {
	Client  *taiga.Client
	Project taiga.Project
	Story   taiga.UserStory
	Ref     taiga.ItemRef
}

func (a *App) storyCommand() *cobra.Command {
	command := &cobra.Command{Use: "story", Aliases: []string{"userstory", "us"}, Short: "Work with Taiga user stories"}
	command.AddCommand(
		a.storyListCommand(),
		a.storyViewCommand(),
		a.storyCreateCommand(),
		a.storyEditCommand(),
		a.storyCloseCommand(),
		a.storyMoveCommand(),
		a.storyAssignCommand(),
		a.storyCommentCommand(),
		a.deleteWorkItemCommand("story"),
		a.watchCommand("story", true),
		a.watchCommand("story", false),
		a.historyCommand("story"),
		a.voteCommand("story", true), a.voteCommand("story", false),
		a.participantCommand("story", "watchers"), a.participantCommand("story", "voters"),
	)
	return command
}

func (a *App) storyListCommand() *cobra.Command {
	var page, limit int
	var sprint, orderBy string
	command := &cobra.Command{
		Use:   "list",
		Short: "List user stories in the selected project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 1000 || page < 1 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			milestone, backlog, err := a.resolveSprintFilter(cmd.Context(), client, project.ID, sprint)
			if err != nil {
				return err
			}
			if err := validateStoryOrderBy(orderBy); err != nil {
				return err
			}
			stories, pagination, err := client.ListUserStories(cmd.Context(), project.ID, page, limit, milestone, backlog, orderBy)
			if err != nil {
				return err
			}
			views := make([]storyView, 0, len(stories))
			for _, story := range stories {
				views = append(views, makeStoryView(story, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "REF\tSUBJECT\tSTATUS\tSPRINT\tPOINTS\tVERSION")
			for _, story := range views {
				points := "-"
				if story.TotalPoints != nil {
					points = fmt.Sprintf("%g", *story.TotalPoints)
				}
				_, _ = fmt.Fprintf(writer, "#%d\t%s\t%s\t%s\t%s\t%d\n", story.Ref, story.Subject, story.Status, story.Sprint, points, story.Version)
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum stories to return")
	command.Flags().StringVar(&sprint, "sprint", "", "filter by sprint name/slug, or backlog")
	command.Flags().StringVar(&orderBy, "order-by", "backlog_order", "order field, prefix with - for descending")
	_ = command.RegisterFlagCompletionFunc("sprint", a.completeSprints)
	return command
}

func (a *App) storyViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "view <ref|project#ref|url>",
		Short: "View a user story",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadStoryTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			view := makeStoryView(target.Story, target.Project.Slug)
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			points := "-"
			if view.TotalPoints != nil {
				points = fmt.Sprintf("%g", *view.TotalPoints)
			}
			_, _ = fmt.Fprintf(a.Out, "#%d  %s\nStatus:   %s\nSprint:   %s\nPoints:   %s\nVersion:  %d\n\n%s\n", view.Ref, view.Subject, view.Status, view.Sprint, points, view.Version, view.Description)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeStories
	return command
}

func (a *App) storyCreateCommand() *cobra.Command {
	var subject, description, status, sprint string
	var dryRun bool
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a user story",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(subject) == "" {
				return usageError("--subject is required")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			request := taiga.CreateUserStoryRequest{Project: project.ID, Subject: subject, Description: description}
			if status != "" {
				selected, err := a.resolveStoryStatus(cmd.Context(), client, project.ID, status, false)
				if err != nil {
					return err
				}
				request.Status = &selected.ID
			}
			if sprint != "" && !strings.EqualFold(strings.TrimSpace(sprint), "backlog") {
				selected, err := a.resolveMilestone(cmd.Context(), client, project.ID, sprint)
				if err != nil {
					return err
				}
				request.Milestone = &selected.ID
			}
			if dryRun {
				return a.renderDryRun("create story", project.Slug, map[string]any{"subject": subject, "description": description, "status": status, "sprint": sprint})
			}
			story, err := client.CreateUserStory(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderStoryMutation("Created", makeStoryView(story, project.Slug))
		},
	}
	command.Flags().StringVar(&subject, "subject", "", "story subject")
	command.Flags().StringVar(&description, "description", "", "story description")
	command.Flags().StringVar(&status, "status", "", "story status name")
	command.Flags().StringVar(&sprint, "sprint", "", "sprint name/slug, or backlog")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	_ = command.RegisterFlagCompletionFunc("status", a.completeStoryStatuses)
	_ = command.RegisterFlagCompletionFunc("sprint", a.completeSprints)
	return command
}

type editStoryOptions struct {
	Subject, Description, Status, Sprint string
	BaseVersion                          int
	DryRun                               bool
}

func (a *App) storyEditCommand() *cobra.Command {
	var options editStoryOptions
	command := &cobra.Command{
		Use:   "edit <ref|project#ref|url>",
		Short: "Edit a user story with optimistic concurrency control",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.BaseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			changed := false
			for _, name := range []string{"subject", "description", "status", "sprint"} {
				changed = changed || cmd.Flags().Changed(name)
			}
			if !changed {
				return usageError("at least one edit flag is required")
			}
			target, err := a.loadStoryTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			request := taiga.UpdateUserStoryRequest{Version: target.Story.Version}
			if options.BaseVersion > 0 {
				request.Version = options.BaseVersion
			}
			if cmd.Flags().Changed("subject") {
				request.Subject = &options.Subject
			}
			if cmd.Flags().Changed("description") {
				request.Description = &options.Description
			}
			if cmd.Flags().Changed("status") {
				selected, err := a.resolveStoryStatus(cmd.Context(), target.Client, target.Project.ID, options.Status, false)
				if err != nil {
					return err
				}
				request.Status = &selected.ID
			}
			if cmd.Flags().Changed("sprint") {
				milestone, err := a.storyMilestoneMutation(cmd.Context(), target.Client, target.Project.ID, options.Sprint)
				if err != nil {
					return err
				}
				request.Milestone = milestone
			}
			if options.DryRun {
				return a.renderDryRun("edit story", fmt.Sprintf("%s#%d", target.Project.Slug, target.Story.Ref), map[string]any{"base_version": request.Version, "subject": request.Subject, "description": request.Description, "status": options.Status, "sprint": options.Sprint})
			}
			updated, err := target.Client.UpdateUserStory(cmd.Context(), target.Story.ID, request)
			if err != nil {
				return err
			}
			return a.renderStoryMutation("Updated", makeStoryView(updated, target.Project.Slug))
		},
	}
	addStoryEditFlags(command, &options)
	command.ValidArgsFunction = a.completeStories
	_ = command.RegisterFlagCompletionFunc("status", a.completeStoryStatuses)
	_ = command.RegisterFlagCompletionFunc("sprint", a.completeSprints)
	return command
}

func addStoryEditFlags(command *cobra.Command, options *editStoryOptions) {
	command.Flags().StringVar(&options.Subject, "subject", "", "new subject")
	command.Flags().StringVar(&options.Description, "description", "", "new description")
	command.Flags().StringVar(&options.Status, "status", "", "status name")
	command.Flags().StringVar(&options.Sprint, "sprint", "", "sprint name/slug, or backlog")
	command.Flags().IntVar(&options.BaseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "resolve and display the mutation without writing")
}

func (a *App) storyCloseCommand() *cobra.Command {
	return a.closeWorkItemCommand(closeCommandSpec{
		use:              "close <ref|project#ref|url>",
		short:            "Move a user story to a closed status",
		statusHelp:       "closed status name; required when the project has multiple closed statuses in non-interactive mode",
		dryRunAction:     "close story",
		completeItems:    a.completeStories,
		completeStatuses: a.completeStoryStatuses,
		plan: func(ctx context.Context, argument, status string) (closePlan, error) {
			target, err := a.loadStoryTarget(ctx, argument)
			if err != nil {
				return closePlan{}, err
			}
			selected, err := a.resolveStoryCloseStatus(ctx, target.Client, target.Project.ID, status)
			if err != nil {
				return closePlan{}, err
			}
			return closePlan{client: target.Client, project: target.Project, itemID: target.Story.ID, ref: target.Story.Ref, version: target.Story.Version, statusID: selected.ID, statusName: selected.Name}, nil
		},
		apply: func(ctx context.Context, plan closePlan) error {
			updated, err := plan.client.UpdateUserStory(ctx, plan.itemID, taiga.UpdateUserStoryRequest{Version: plan.version, Status: &plan.statusID})
			if err != nil {
				return err
			}
			if err := a.renderStoryMutation("Closed", makeStoryView(updated, plan.project.Slug)); err != nil {
				return err
			}
			// A story stays open while any of its tasks is, so a closed status
			// alone does not mean what someone would assume it means.
			if !a.global.JSON && !updated.IsClosed {
				_, _ = fmt.Fprintln(a.Err, "Warning: status changed to closed, but open tasks keep this story active")
			}
			return nil
		},
	})
}

func (a *App) storyAssignCommand() *cobra.Command {
	var assignees []string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use:   "assign <ref|project#ref|url>",
		Short: "Replace all user story assignees",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if len(assignees) == 0 {
				return usageError("at least one --to value is required")
			}
			target, err := a.loadStoryTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			userIDs, err := a.resolveStoryAssignees(cmd.Context(), target.Client, target.Project.ID, assignees)
			if err != nil {
				return err
			}
			version := target.Story.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("assign story", fmt.Sprintf("%s#%d", target.Project.Slug, target.Story.Ref), map[string]any{"assignees": assignees, "assigned_user_ids": userIDs, "base_version": version})
			}
			updated, err := target.Client.UpdateUserStory(cmd.Context(), target.Story.ID, taiga.UpdateUserStoryRequest{Version: version, AssignedUsers: &userIDs})
			if err != nil {
				return err
			}
			return a.renderStoryMutation("Assigned", makeStoryView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringSliceVar(&assignees, "to", nil, "assignee username/full name; repeat or comma-separate for multiple users")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeStories
	_ = command.RegisterFlagCompletionFunc("to", a.completeMembers)
	return command
}

func (a *App) storyMoveCommand() *cobra.Command {
	var sprint string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use:   "move <ref|project#ref|url>",
		Short: "Move a user story to a sprint or backlog",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if strings.TrimSpace(sprint) == "" {
				return usageError("--sprint is required; use backlog to remove the story from a sprint")
			}
			target, err := a.loadStoryTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			milestone, err := a.storyMilestoneMutation(cmd.Context(), target.Client, target.Project.ID, sprint)
			if err != nil {
				return err
			}
			version := target.Story.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("move story", fmt.Sprintf("%s#%d", target.Project.Slug, target.Story.Ref), map[string]any{"sprint": sprint, "base_version": version})
			}
			updated, err := target.Client.UpdateUserStory(cmd.Context(), target.Story.ID, taiga.UpdateUserStoryRequest{Version: version, Milestone: milestone})
			if err != nil {
				return err
			}
			return a.renderStoryMutation("Moved", makeStoryView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&sprint, "sprint", "", "sprint name/slug, or backlog")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeStories
	_ = command.RegisterFlagCompletionFunc("sprint", a.completeSprints)
	return command
}

func (a *App) storyCommentCommand() *cobra.Command {
	return a.commentWorkItemCommand(commentCommandSpec{
		use:           "comment <ref|project#ref|url>",
		short:         "Comment on a user story",
		dryRunAction:  "comment on story",
		body:          genericBody,
		completeItems: a.completeStories,
		load: func(ctx context.Context, argument string) (commentPlan, error) {
			target, err := a.loadStoryTarget(ctx, argument)
			if err != nil {
				return commentPlan{}, err
			}
			return commentPlan{client: target.Client, project: target.Project, itemID: target.Story.ID, ref: target.Story.Ref, version: target.Story.Version}, nil
		},
		apply: func(ctx context.Context, plan commentPlan, body string) error {
			updated, err := plan.client.UpdateUserStory(ctx, plan.itemID, taiga.UpdateUserStoryRequest{Version: plan.version, Comment: &body})
			if err != nil {
				return err
			}
			return a.renderStoryMutation("Commented on", makeStoryView(updated, plan.project.Slug))
		},
	})
}

func (a *App) loadStoryTarget(ctx context.Context, value string) (storyTarget, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return storyTarget{}, err
	}
	ref, err := taiga.ParseStoryRef(value, settings.Project)
	if err != nil {
		return storyTarget{}, validationError("invalid_ref", err.Error())
	}
	project, err := client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return storyTarget{}, err
	}
	story, err := client.GetUserStoryByRef(ctx, project.Slug, ref.Ref)
	if err != nil {
		return storyTarget{}, err
	}
	return storyTarget{Client: client, Project: project, Story: story, Ref: ref}, nil
}

func makeStoryView(story taiga.UserStory, projectSlug string) storyView {
	return storyView{
		ID: story.ID, Ref: story.Ref, Project: projectSlug, Subject: story.Subject,
		Description: story.Description, Version: story.Version, Status: story.StatusExtraInfo.Name,
		Sprint: story.MilestoneName, SprintSlug: story.MilestoneSlug, AssignedUsers: story.AssignedUsers,
		TotalPoints: story.TotalPoints, Points: story.Points, IsClosed: story.IsClosed,
		IsBlocked: story.IsBlocked, IsWatcher: story.IsWatcher, CreatedDate: story.CreatedDate, ModifiedDate: story.ModifiedDate,
	}
}

func (a *App) resolveStoryStatus(ctx context.Context, client *taiga.Client, projectID int64, name string, requireClosed bool) (taiga.UserStoryStatus, error) {
	values, err := client.UserStoryStatuses(ctx, projectID)
	if err != nil {
		return taiga.UserStoryStatus{}, err
	}
	matches := []taiga.UserStoryStatus{}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) && (!requireClosed || value.IsClosed) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.UserStoryStatus{}, validationError("unknown_status", fmt.Sprintf("user story status %q was not found", name))
	}
	return taiga.UserStoryStatus{}, validationError("ambiguous_status", fmt.Sprintf("user story status %q matches multiple values", name))
}

func (a *App) resolveStoryCloseStatus(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.UserStoryStatus, error) {
	if name != "" {
		return a.resolveStoryStatus(ctx, client, projectID, name, true)
	}
	values, err := client.UserStoryStatuses(ctx, projectID)
	if err != nil {
		return taiga.UserStoryStatus{}, err
	}
	closed := []taiga.UserStoryStatus{}
	for _, value := range values {
		if value.IsClosed {
			closed = append(closed, value)
		}
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].Order < closed[j].Order })
	if len(closed) == 1 {
		return closed[0], nil
	}
	if len(closed) == 0 {
		return taiga.UserStoryStatus{}, validationError("missing_closed_status", "project has no closed user story status")
	}
	if a.global.NoInput || !a.stdinTTY() {
		names := make([]string, 0, len(closed))
		for _, value := range closed {
			names = append(names, value.Name)
		}
		return taiga.UserStoryStatus{}, validationError("ambiguous_status", "multiple closed statuses are available; pass --status: "+strings.Join(names, ", "))
	}
	_, _ = fmt.Fprintln(a.Err, "Closed statuses:")
	for _, value := range closed {
		_, _ = fmt.Fprintf(a.Err, "  %s\n", value.Name)
	}
	selected, err := a.readLine("Status: ")
	if err != nil {
		return taiga.UserStoryStatus{}, err
	}
	return a.resolveStoryStatus(ctx, client, projectID, selected, true)
}

func (a *App) resolveMilestone(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.Milestone, error) {
	values, err := client.Milestones(ctx, projectID, nil)
	if err != nil {
		return taiga.Milestone{}, err
	}
	matches := []taiga.Milestone{}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) || strings.EqualFold(strings.TrimSpace(value.Slug), strings.TrimSpace(name)) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.Milestone{}, validationError("unknown_sprint", fmt.Sprintf("sprint %q was not found", name))
	}
	return taiga.Milestone{}, validationError("ambiguous_sprint", fmt.Sprintf("sprint %q matches multiple milestones", name))
}

func (a *App) resolveSprintFilter(ctx context.Context, client *taiga.Client, projectID int64, name string) (*int64, bool, error) {
	if strings.TrimSpace(name) == "" {
		return nil, false, nil
	}
	if strings.EqualFold(strings.TrimSpace(name), "backlog") {
		return nil, true, nil
	}
	selected, err := a.resolveMilestone(ctx, client, projectID, name)
	if err != nil {
		return nil, false, err
	}
	return &selected.ID, false, nil
}

func (a *App) storyMilestoneMutation(ctx context.Context, client *taiga.Client, projectID int64, name string) (**int64, error) {
	var milestoneID *int64
	if !strings.EqualFold(strings.TrimSpace(name), "backlog") {
		selected, err := a.resolveMilestone(ctx, client, projectID, name)
		if err != nil {
			return nil, err
		}
		milestoneID = &selected.ID
	}
	return &milestoneID, nil
}

func (a *App) resolveStoryAssignees(ctx context.Context, client *taiga.Client, projectID int64, names []string) ([]int64, error) {
	users, err := client.ListProjectUsers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]int64, 0, len(names))
	seen := map[int64]struct{}{}
	for _, name := range names {
		matches := []int64{}
		for _, user := range users {
			if strings.EqualFold(strings.TrimSpace(user.Username), strings.TrimSpace(name)) || strings.EqualFold(strings.TrimSpace(user.FullName), strings.TrimSpace(name)) {
				matches = append(matches, user.ID)
			}
		}
		if len(matches) == 0 {
			return nil, validationError("unknown_assignee", fmt.Sprintf("project member %q was not found", name))
		}
		if len(matches) > 1 {
			return nil, validationError("ambiguous_assignee", fmt.Sprintf("project member %q matches multiple users", name))
		}
		if _, ok := seen[matches[0]]; !ok {
			seen[matches[0]] = struct{}{}
			result = append(result, matches[0])
		}
	}
	return result, nil
}

func validateStoryOrderBy(value string) error {
	field := strings.TrimPrefix(strings.TrimSpace(value), "-")
	allowed := map[string]struct{}{
		"backlog_order": {}, "sprint_order": {}, "kanban_order": {}, "epic_order": {},
		"project": {}, "milestone": {}, "status": {}, "created_date": {},
		"modified_date": {}, "assigned_to": {}, "subject": {}, "total_voters": {},
	}
	if _, ok := allowed[field]; !ok {
		return usageError(fmt.Sprintf("unsupported story order field %q", value))
	}
	return nil
}

func (a *App) renderStoryMutation(verb string, view storyView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s story %s#%d: %s (version %d)\n", verb, view.Project, view.Ref, view.Subject, view.Version)
	}
	return nil
}
