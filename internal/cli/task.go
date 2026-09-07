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

type taskView struct {
	ID           int64  `json:"id"`
	Ref          int    `json:"ref"`
	Project      string `json:"project"`
	StoryRef     int    `json:"story_ref,omitempty"`
	StorySubject string `json:"story_subject,omitempty"`
	Subject      string `json:"subject"`
	Description  string `json:"description,omitempty"`
	Version      int    `json:"version"`
	Status       string `json:"status"`
	SprintSlug   string `json:"sprint_slug,omitempty"`
	Assignee     string `json:"assignee,omitempty"`
	IsClosed     bool   `json:"is_closed"`
	IsWatcher    bool   `json:"is_watcher"`
	IsBlocked    bool   `json:"is_blocked"`
	CreatedDate  string `json:"created_date,omitempty"`
	ModifiedDate string `json:"modified_date,omitempty"`
	FinishedDate string `json:"finished_date,omitempty"`
}

type taskTarget struct {
	Client  *taiga.Client
	Project taiga.Project
	Task    taiga.Task
	Ref     taiga.ItemRef
}

func (a *App) taskCommand() *cobra.Command {
	command := &cobra.Command{Use: "task", Short: "Work with Taiga tasks"}
	command.AddCommand(
		a.taskListCommand(), a.taskViewCommand(), a.taskCreateCommand(),
		a.taskEditCommand(), a.taskDoneCommand(), a.taskReopenCommand(),
		a.taskAssignCommand(), a.taskUnassignCommand(), a.taskMoveCommand(), a.taskCommentCommand(),
		a.deleteWorkItemCommand("task"),
		a.watchCommand("task", true), a.watchCommand("task", false), a.historyCommand("task"),
		a.voteCommand("task", true), a.voteCommand("task", false),
		a.participantCommand("task", "watchers"), a.participantCommand("task", "voters"),
	)
	return command
}

func (a *App) taskListCommand() *cobra.Command {
	var story, orderBy string
	var page, limit int
	command := &cobra.Command{
		Use:   "list",
		Short: "List tasks in the selected project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 1000 || page < 1 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			if err := validateTaskOrderBy(orderBy); err != nil {
				return err
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			var storyID *int64
			if story != "" {
				parent, err := a.resolveParentStory(cmd.Context(), client, project.Slug, story)
				if err != nil {
					return err
				}
				storyID = &parent.ID
			}
			tasks, pagination, err := client.ListTasks(cmd.Context(), project.ID, storyID, page, limit, orderBy)
			if err != nil {
				return err
			}
			views := make([]taskView, 0, len(tasks))
			for _, task := range tasks {
				views = append(views, makeTaskView(task, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "REF\tSUBJECT\tSTATUS\tSTORY\tASSIGNEE\tVERSION")
			for _, task := range views {
				_, _ = fmt.Fprintf(writer, "#%d\t%s\t%s\t#%d\t%s\t%d\n", task.Ref, task.Subject, task.Status, task.StoryRef, task.Assignee, task.Version)
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().StringVar(&story, "story", "", "filter by parent Story ref")
	command.Flags().StringVar(&orderBy, "order-by", "us_order", "order field, prefix with - for descending")
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum tasks to return")
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	return command
}

func (a *App) taskViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "view <ref|project#ref|url>", Short: "View a task", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			view := makeTaskView(target.Task, target.Project.Slug)
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "#%d  %s\nStatus:    %s\nStory:     #%d %s\nSprint:    %s\nAssignee:  %s\nVersion:   %d\n\n%s\n", view.Ref, view.Subject, view.Status, view.StoryRef, view.StorySubject, view.SprintSlug, view.Assignee, view.Version, view.Description)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeTasks
	return command
}

func (a *App) taskCreateCommand() *cobra.Command {
	var subject, description, story, status, assignee string
	var dryRun bool
	command := &cobra.Command{
		Use: "create", Short: "Create a task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(subject) == "" {
				return usageError("--subject is required")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			request := taiga.CreateTaskRequest{Project: project.ID, Subject: subject, Description: description}
			if story != "" {
				parent, err := a.resolveParentStory(cmd.Context(), client, project.Slug, story)
				if err != nil {
					return err
				}
				request.UserStory = &parent.ID
			}
			if status != "" {
				selected, err := a.resolveTaskStatus(cmd.Context(), client, project.ID, status, false)
				if err != nil {
					return err
				}
				request.Status = &selected.ID
			}
			if assignee != "" {
				userID, err := a.resolveAssignee(cmd.Context(), client, project.ID, assignee)
				if err != nil {
					return err
				}
				request.AssignedTo = &userID
			}
			if dryRun {
				return a.renderDryRun("create task", project.Slug, map[string]any{"subject": subject, "description": description, "story": story, "status": status, "assignee": assignee})
			}
			task, err := client.CreateTask(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Created", makeTaskView(task, project.Slug))
		},
	}
	command.Flags().StringVar(&subject, "subject", "", "task subject")
	command.Flags().StringVar(&description, "description", "", "task description")
	command.Flags().StringVar(&story, "story", "", "parent Story ref")
	command.Flags().StringVar(&status, "status", "", "task status name")
	command.Flags().StringVar(&assignee, "assignee", "", "assignee username or full name")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	_ = command.RegisterFlagCompletionFunc("status", a.completeTaskStatuses)
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	return command
}

type editTaskOptions struct {
	Subject, Description, Story, Status, Assignee string
	BaseVersion                                   int
	DryRun                                        bool
}

func (a *App) taskEditCommand() *cobra.Command {
	var options editTaskOptions
	command := &cobra.Command{
		Use: "edit <ref|project#ref|url>", Short: "Edit a task with optimistic concurrency control", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.BaseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			changed := false
			for _, name := range []string{"subject", "description", "story", "status", "assignee"} {
				changed = changed || cmd.Flags().Changed(name)
			}
			if !changed {
				return usageError("at least one edit flag is required")
			}
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			request := taiga.UpdateTaskRequest{Version: target.Task.Version}
			if options.BaseVersion > 0 {
				request.Version = options.BaseVersion
			}
			if cmd.Flags().Changed("subject") {
				request.Subject = &options.Subject
			}
			if cmd.Flags().Changed("description") {
				request.Description = &options.Description
			}
			if cmd.Flags().Changed("story") {
				parent, err := a.resolveParentStory(cmd.Context(), target.Client, target.Project.Slug, options.Story)
				if err != nil {
					return err
				}
				storyID := &parent.ID
				request.UserStory = &storyID
			}
			if cmd.Flags().Changed("status") {
				selected, err := a.resolveTaskStatus(cmd.Context(), target.Client, target.Project.ID, options.Status, false)
				if err != nil {
					return err
				}
				request.Status = &selected.ID
			}
			if cmd.Flags().Changed("assignee") {
				userID, err := a.resolveAssignee(cmd.Context(), target.Client, target.Project.ID, options.Assignee)
				if err != nil {
					return err
				}
				assignedTo := &userID
				request.AssignedTo = &assignedTo
			}
			if options.DryRun {
				return a.renderDryRun("edit task", fmt.Sprintf("%s#%d", target.Project.Slug, target.Task.Ref), map[string]any{"base_version": request.Version, "subject": request.Subject, "description": request.Description, "story": options.Story, "status": options.Status, "assignee": options.Assignee})
			}
			updated, err := target.Client.UpdateTask(cmd.Context(), target.Task.ID, request)
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Updated", makeTaskView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&options.Subject, "subject", "", "new subject")
	command.Flags().StringVar(&options.Description, "description", "", "new description")
	command.Flags().StringVar(&options.Story, "story", "", "new parent Story ref")
	command.Flags().StringVar(&options.Status, "status", "", "status name")
	command.Flags().StringVar(&options.Assignee, "assignee", "", "assignee username or full name")
	command.Flags().IntVar(&options.BaseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeTasks
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	_ = command.RegisterFlagCompletionFunc("status", a.completeTaskStatuses)
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	return command
}

func (a *App) taskDoneCommand() *cobra.Command {
	return a.closeWorkItemCommand(closeCommandSpec{
		use:              "done <ref|project#ref|url>",
		short:            "Move a task to a closed status",
		statusHelp:       "closed status name when the project has multiple closed statuses",
		dryRunAction:     "complete task",
		completeItems:    a.completeTasks,
		completeStatuses: a.completeTaskStatuses,
		plan: func(ctx context.Context, argument, status string) (closePlan, error) {
			target, err := a.loadTaskTarget(ctx, argument)
			if err != nil {
				return closePlan{}, err
			}
			selected, err := a.resolveTaskCloseStatus(ctx, target.Client, target.Project.ID, status)
			if err != nil {
				return closePlan{}, err
			}
			return closePlan{client: target.Client, project: target.Project, itemID: target.Task.ID, ref: target.Task.Ref, version: target.Task.Version, statusID: selected.ID, statusName: selected.Name}, nil
		},
		apply: func(ctx context.Context, plan closePlan) error {
			updated, err := plan.client.UpdateTask(ctx, plan.itemID, taiga.UpdateTaskRequest{Version: plan.version, Status: &plan.statusID})
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Completed", makeTaskView(updated, plan.project.Slug))
		},
	})
}

func (a *App) taskReopenCommand() *cobra.Command {
	var status string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use: "reopen <ref|project#ref|url>", Short: "Move a task to an open status", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			selected, err := a.resolveTaskOpenStatus(cmd.Context(), target.Client, target.Project.ID, status)
			if err != nil {
				return err
			}
			version := target.Task.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("reopen task", fmt.Sprintf("%s#%d", target.Project.Slug, target.Task.Ref), map[string]any{"status": selected.Name, "base_version": version})
			}
			updated, err := target.Client.UpdateTask(cmd.Context(), target.Task.ID, taiga.UpdateTaskRequest{Version: version, Status: &selected.ID})
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Reopened", makeTaskView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&status, "status", "", "open status name when the project has multiple open statuses")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeTasks
	_ = command.RegisterFlagCompletionFunc("status", a.completeTaskStatuses)
	return command
}

func (a *App) taskAssignCommand() *cobra.Command {
	var assignee string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use: "assign <ref|project#ref|url>", Short: "Assign a task", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if strings.TrimSpace(assignee) == "" {
				return usageError("--to is required")
			}
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			userID, err := a.resolveAssignee(cmd.Context(), target.Client, target.Project.ID, assignee)
			if err != nil {
				return err
			}
			version := target.Task.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("assign task", fmt.Sprintf("%s#%d", target.Project.Slug, target.Task.Ref), map[string]any{"assignee": assignee, "assigned_user_id": userID, "base_version": version})
			}
			assignedTo := &userID
			updated, err := target.Client.UpdateTask(cmd.Context(), target.Task.ID, taiga.UpdateTaskRequest{Version: version, AssignedTo: &assignedTo})
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Assigned", makeTaskView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&assignee, "to", "", "assignee username or full name")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeTasks
	_ = command.RegisterFlagCompletionFunc("to", a.completeMembers)
	return command
}

func (a *App) taskUnassignCommand() *cobra.Command {
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use: "unassign <ref|project#ref|url>", Short: "Remove a task assignee", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			version := target.Task.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("unassign task", fmt.Sprintf("%s#%d", target.Project.Slug, target.Task.Ref), map[string]any{"assigned_to": nil, "base_version": version})
			}
			var assignedTo *int64
			updated, err := target.Client.UpdateTask(cmd.Context(), target.Task.ID, taiga.UpdateTaskRequest{Version: version, AssignedTo: &assignedTo})
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Unassigned", makeTaskView(updated, target.Project.Slug))
		},
	}
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeTasks
	return command
}

func (a *App) taskMoveCommand() *cobra.Command {
	var story, sprint string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use: "move <ref|project#ref|url>", Short: "Move a task to a Story, Sprint, or backlog", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if (story == "") == (sprint == "") {
				return usageError("exactly one of --story or --sprint is required")
			}
			target, err := a.loadTaskTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			version := target.Task.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			request := taiga.UpdateTaskRequest{Version: version}
			changes := map[string]any{"base_version": version}
			if story != "" {
				parent, err := a.resolveParentStory(cmd.Context(), target.Client, target.Project.Slug, story)
				if err != nil {
					return err
				}
				storyID := &parent.ID
				request.UserStory = &storyID
				changes["story"] = story
				changes["story_id"] = parent.ID
			} else {
				var storyID *int64
				request.UserStory = &storyID
				var milestoneID *int64
				if !strings.EqualFold(strings.TrimSpace(sprint), "backlog") {
					selected, err := a.resolveMilestone(cmd.Context(), target.Client, target.Project.ID, sprint)
					if err != nil {
						return err
					}
					milestoneID = &selected.ID
					changes["sprint_id"] = selected.ID
				}
				request.Milestone = &milestoneID
				changes["sprint"] = sprint
			}
			if dryRun {
				return a.renderDryRun("move task", fmt.Sprintf("%s#%d", target.Project.Slug, target.Task.Ref), changes)
			}
			updated, err := target.Client.UpdateTask(cmd.Context(), target.Task.ID, request)
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Moved", makeTaskView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&story, "story", "", "parent Story ref")
	command.Flags().StringVar(&sprint, "sprint", "", "standalone Sprint name/slug, or backlog")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeTasks
	_ = command.RegisterFlagCompletionFunc("story", a.completeStories)
	_ = command.RegisterFlagCompletionFunc("sprint", a.completeSprints)
	return command
}

func (a *App) taskCommentCommand() *cobra.Command {
	return a.commentWorkItemCommand(commentCommandSpec{
		use:           "comment <ref|project#ref|url>",
		short:         "Comment on a task",
		dryRunAction:  "comment on task",
		body:          genericBody,
		completeItems: a.completeTasks,
		load: func(ctx context.Context, argument string) (commentPlan, error) {
			target, err := a.loadTaskTarget(ctx, argument)
			if err != nil {
				return commentPlan{}, err
			}
			return commentPlan{client: target.Client, project: target.Project, itemID: target.Task.ID, ref: target.Task.Ref, version: target.Task.Version}, nil
		},
		apply: func(ctx context.Context, plan commentPlan, body string) error {
			updated, err := plan.client.UpdateTask(ctx, plan.itemID, taiga.UpdateTaskRequest{Version: plan.version, Comment: &body})
			if err != nil {
				return err
			}
			return a.renderTaskMutation("Commented on", makeTaskView(updated, plan.project.Slug))
		},
	})
}

func (a *App) loadTaskTarget(ctx context.Context, value string) (taskTarget, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return taskTarget{}, err
	}
	ref, err := taiga.ParseTaskRef(value, settings.Project)
	if err != nil {
		return taskTarget{}, validationError("invalid_ref", err.Error())
	}
	project, err := client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return taskTarget{}, err
	}
	task, err := client.GetTaskByRef(ctx, project.Slug, ref.Ref)
	if err != nil {
		return taskTarget{}, err
	}
	return taskTarget{Client: client, Project: project, Task: task, Ref: ref}, nil
}

func (a *App) resolveParentStory(ctx context.Context, client *taiga.Client, projectSlug, value string) (taiga.UserStory, error) {
	ref, err := taiga.ParseStoryRef(value, projectSlug)
	if err != nil {
		return taiga.UserStory{}, validationError("invalid_story_ref", err.Error())
	}
	if ref.Project != projectSlug {
		return taiga.UserStory{}, validationError("cross_project_story", "parent Story must belong to the selected project")
	}
	return client.GetUserStoryByRef(ctx, projectSlug, ref.Ref)
}

func makeTaskView(task taiga.Task, projectSlug string) taskView {
	view := taskView{ID: task.ID, Ref: task.Ref, Project: projectSlug, Subject: task.Subject, Description: task.Description, Version: task.Version, Status: task.StatusExtraInfo.Name, SprintSlug: task.MilestoneSlug, IsClosed: task.IsClosed, IsWatcher: task.IsWatcher, IsBlocked: task.IsBlocked, CreatedDate: task.CreatedDate, ModifiedDate: task.ModifiedDate, FinishedDate: task.FinishedDate}
	if task.UserStoryExtraInfo != nil {
		view.StoryRef = task.UserStoryExtraInfo.Ref
		view.StorySubject = task.UserStoryExtraInfo.Subject
	}
	if task.AssignedToExtraInfo != nil {
		view.Assignee = firstNonEmpty(task.AssignedToExtraInfo.Username, task.AssignedToExtraInfo.FullName)
	}
	return view
}

func (a *App) resolveTaskStatus(ctx context.Context, client *taiga.Client, projectID int64, name string, requireClosed bool) (taiga.TaskStatus, error) {
	values, err := client.TaskStatuses(ctx, projectID)
	if err != nil {
		return taiga.TaskStatus{}, err
	}
	matches := []taiga.TaskStatus{}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) && (!requireClosed || value.IsClosed) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.TaskStatus{}, validationError("unknown_status", fmt.Sprintf("task status %q was not found", name))
	}
	return taiga.TaskStatus{}, validationError("ambiguous_status", fmt.Sprintf("task status %q matches multiple values", name))
}

func (a *App) resolveTaskCloseStatus(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.TaskStatus, error) {
	if name != "" {
		return a.resolveTaskStatus(ctx, client, projectID, name, true)
	}
	values, err := client.TaskStatuses(ctx, projectID)
	if err != nil {
		return taiga.TaskStatus{}, err
	}
	closed := []taiga.TaskStatus{}
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
		return taiga.TaskStatus{}, validationError("missing_closed_status", "project has no closed task status")
	}
	if a.global.NoInput || !a.stdinTTY() {
		names := make([]string, 0, len(closed))
		for _, value := range closed {
			names = append(names, value.Name)
		}
		return taiga.TaskStatus{}, validationError("ambiguous_status", "multiple closed statuses are available; pass --status: "+strings.Join(names, ", "))
	}
	_, _ = fmt.Fprintln(a.Err, "Closed statuses:")
	for _, value := range closed {
		_, _ = fmt.Fprintf(a.Err, "  %s\n", value.Name)
	}
	selected, err := a.readLine("Status: ")
	if err != nil {
		return taiga.TaskStatus{}, err
	}
	return a.resolveTaskStatus(ctx, client, projectID, selected, true)
}

func (a *App) resolveTaskOpenStatus(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.TaskStatus, error) {
	if name != "" {
		values, err := client.TaskStatuses(ctx, projectID)
		if err != nil {
			return taiga.TaskStatus{}, err
		}
		matches := []taiga.TaskStatus{}
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) && !value.IsClosed {
				matches = append(matches, value)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) == 0 {
			return taiga.TaskStatus{}, validationError("unknown_status", fmt.Sprintf("open task status %q was not found", name))
		}
		return taiga.TaskStatus{}, validationError("ambiguous_status", fmt.Sprintf("open task status %q matches multiple values", name))
	}
	values, err := client.TaskStatuses(ctx, projectID)
	if err != nil {
		return taiga.TaskStatus{}, err
	}
	open := []taiga.TaskStatus{}
	for _, value := range values {
		if !value.IsClosed {
			open = append(open, value)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Order < open[j].Order })
	if len(open) == 1 {
		return open[0], nil
	}
	if len(open) == 0 {
		return taiga.TaskStatus{}, validationError("missing_open_status", "project has no open task status")
	}
	if a.global.NoInput || !a.stdinTTY() {
		names := make([]string, 0, len(open))
		for _, value := range open {
			names = append(names, value.Name)
		}
		return taiga.TaskStatus{}, validationError("ambiguous_status", "multiple open statuses are available; pass --status: "+strings.Join(names, ", "))
	}
	_, _ = fmt.Fprintln(a.Err, "Open statuses:")
	for _, value := range open {
		_, _ = fmt.Fprintf(a.Err, "  %s\n", value.Name)
	}
	selected, err := a.readLine("Status: ")
	if err != nil {
		return taiga.TaskStatus{}, err
	}
	return a.resolveTaskOpenStatus(ctx, client, projectID, selected)
}

func validateTaskOrderBy(value string) error {
	field := strings.TrimPrefix(strings.TrimSpace(value), "-")
	allowed := map[string]struct{}{
		"project": {}, "milestone": {}, "status": {}, "created_date": {}, "modified_date": {},
		"assigned_to": {}, "us_order": {}, "subject": {}, "total_voters": {},
	}
	if _, ok := allowed[field]; !ok {
		return usageError(fmt.Sprintf("unsupported task order field %q", value))
	}
	return nil
}

func (a *App) renderTaskMutation(verb string, view taskView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s task %s#%d: %s (version %d)\n", verb, view.Project, view.Ref, view.Subject, view.Version)
	}
	return nil
}
