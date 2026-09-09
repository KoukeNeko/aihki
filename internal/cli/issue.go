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

type issueView struct {
	ID           int64  `json:"id"`
	Ref          int    `json:"ref"`
	Project      string `json:"project"`
	Subject      string `json:"subject"`
	Description  string `json:"description,omitempty"`
	Version      int    `json:"version"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	Severity     string `json:"severity"`
	Type         string `json:"type"`
	Assignee     string `json:"assignee,omitempty"`
	IsClosed     bool   `json:"is_closed"`
	IsWatcher    bool   `json:"is_watcher"`
	CreatedDate  string `json:"created_date,omitempty"`
	ModifiedDate string `json:"modified_date,omitempty"`
}

type issueTarget struct {
	Client  *taiga.Client
	Project taiga.Project
	Issue   taiga.Issue
	Ref     taiga.ItemRef
}

func (a *App) issueCommand() *cobra.Command {
	command := &cobra.Command{Use: "issue", Short: "Work with Taiga issues"}
	command.AddCommand(
		a.issueListCommand(),
		a.issueViewCommand(),
		a.issueCreateCommand(),
		a.issueEditCommand(),
		a.issueCloseCommand(),
		a.issueAssignCommand(),
		a.issueCommentCommand(),
		a.deleteWorkItemCommand("issue"),
		a.watchCommand("issue", true),
		a.watchCommand("issue", false),
		a.historyCommand("issue"),
		a.voteCommand("issue", true), a.voteCommand("issue", false),
		a.participantCommand("issue", "watchers"), a.participantCommand("issue", "voters"),
	)
	return command
}

func (a *App) issueListCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use:   "list",
		Short: "List issues in the selected project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 1000 || page < 1 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			issues, pagination, err := client.ListIssues(cmd.Context(), project.ID, page, limit)
			if err != nil {
				return err
			}
			views := make([]issueView, 0, len(issues))
			for _, issue := range issues {
				views = append(views, makeIssueView(issue, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "REF\tSUBJECT\tSTATUS\tASSIGNEE\tVERSION")
			for _, issue := range views {
				_, _ = fmt.Fprintf(writer, "#%d\t%s\t%s\t%s\t%d\n", issue.Ref, issue.Subject, issue.Status, issue.Assignee, issue.Version)
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum issues to return")
	return command
}

func (a *App) issueViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "view <ref|project#ref|url>",
		Short: "View an issue",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadIssueTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			view := makeIssueView(target.Issue, target.Project.Slug)
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "#%d  %s\nStatus:    %s\nType:      %s\nPriority:  %s\nSeverity:  %s\nAssignee:  %s\nVersion:   %d\n\n%s\n", view.Ref, view.Subject, view.Status, view.Type, view.Priority, view.Severity, view.Assignee, view.Version, view.Description)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeIssues
	return command
}

func (a *App) issueCreateCommand() *cobra.Command {
	var subject, description, status, priority, severity, issueType, assignee string
	var dryRun bool
	command := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(subject) == "" {
				return usageError("--subject is required")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			request := taiga.CreateIssueRequest{Project: project.ID, Subject: subject, Description: description}
			if status != "" {
				value, err := a.resolveIssueStatus(cmd.Context(), client, project.ID, status, false)
				if err != nil {
					return err
				}
				request.Status = &value.ID
			}
			if priority != "" {
				value, err := a.resolveNamedMetadata(cmd.Context(), priority, func(ctx context.Context) ([]taiga.NamedMetadata, error) {
					return client.IssuePriorities(ctx, project.ID)
				})
				if err != nil {
					return err
				}
				request.Priority = &value.ID
			}
			if severity != "" {
				value, err := a.resolveNamedMetadata(cmd.Context(), severity, func(ctx context.Context) ([]taiga.NamedMetadata, error) {
					return client.IssueSeverities(ctx, project.ID)
				})
				if err != nil {
					return err
				}
				request.Severity = &value.ID
			}
			if issueType != "" {
				value, err := a.resolveNamedMetadata(cmd.Context(), issueType, func(ctx context.Context) ([]taiga.NamedMetadata, error) { return client.IssueTypes(ctx, project.ID) })
				if err != nil {
					return err
				}
				request.Type = &value.ID
			}
			if assignee != "" {
				userID, err := a.resolveAssignee(cmd.Context(), client, project.ID, assignee)
				if err != nil {
					return err
				}
				request.AssignedTo = &userID
			}
			if dryRun {
				return a.renderDryRun("create", project.Slug, map[string]any{"subject": subject, "description": description, "status": status, "priority": priority, "severity": severity, "type": issueType, "assignee": assignee})
			}
			issue, err := client.CreateIssue(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderIssueMutation("Created", makeIssueView(issue, project.Slug))
		},
	}
	flags := command.Flags()
	flags.StringVar(&subject, "subject", "", "issue subject")
	flags.StringVar(&description, "description", "", "issue description")
	flags.StringVar(&status, "status", "", "issue status name")
	flags.StringVar(&priority, "priority", "", "issue priority name")
	flags.StringVar(&severity, "severity", "", "issue severity name")
	flags.StringVar(&issueType, "type", "", "issue type name")
	flags.StringVar(&assignee, "assignee", "", "assignee username or full name")
	flags.BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	_ = command.RegisterFlagCompletionFunc("status", a.completeIssueStatuses)
	_ = command.RegisterFlagCompletionFunc("priority", a.completeNamedMetadata("issue-priorities", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssuePriorities(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("severity", a.completeNamedMetadata("issue-severities", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssueSeverities(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("type", a.completeNamedMetadata("issue-types", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssueTypes(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	return command
}

type editIssueOptions struct {
	Subject, Description, Status, Priority, Severity, Type, Assignee string
	BaseVersion                                                      int
	DryRun                                                           bool
}

func (a *App) issueEditCommand() *cobra.Command {
	var options editIssueOptions
	command := &cobra.Command{
		Use:   "edit <ref|project#ref|url>",
		Short: "Edit an issue with optimistic concurrency control",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.BaseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			changed := false
			for _, name := range []string{"subject", "description", "status", "priority", "severity", "type", "assignee"} {
				changed = changed || cmd.Flags().Changed(name)
			}
			if !changed {
				return usageError("at least one edit flag is required")
			}
			target, err := a.loadIssueTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			request := taiga.UpdateIssueRequest{Version: target.Issue.Version}
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
				value, err := a.resolveIssueStatus(cmd.Context(), target.Client, target.Project.ID, options.Status, false)
				if err != nil {
					return err
				}
				request.Status = &value.ID
			}
			if cmd.Flags().Changed("priority") {
				value, err := a.resolveNamedMetadata(cmd.Context(), options.Priority, func(ctx context.Context) ([]taiga.NamedMetadata, error) {
					return target.Client.IssuePriorities(ctx, target.Project.ID)
				})
				if err != nil {
					return err
				}
				request.Priority = &value.ID
			}
			if cmd.Flags().Changed("severity") {
				value, err := a.resolveNamedMetadata(cmd.Context(), options.Severity, func(ctx context.Context) ([]taiga.NamedMetadata, error) {
					return target.Client.IssueSeverities(ctx, target.Project.ID)
				})
				if err != nil {
					return err
				}
				request.Severity = &value.ID
			}
			if cmd.Flags().Changed("type") {
				value, err := a.resolveNamedMetadata(cmd.Context(), options.Type, func(ctx context.Context) ([]taiga.NamedMetadata, error) {
					return target.Client.IssueTypes(ctx, target.Project.ID)
				})
				if err != nil {
					return err
				}
				request.Type = &value.ID
			}
			if cmd.Flags().Changed("assignee") {
				userID, err := a.resolveAssignee(cmd.Context(), target.Client, target.Project.ID, options.Assignee)
				if err != nil {
					return err
				}
				request.AssignedTo = &userID
			}
			if options.DryRun {
				return a.renderDryRun("edit", fmt.Sprintf("%s#%d", target.Project.Slug, target.Issue.Ref), map[string]any{"base_version": request.Version, "subject": request.Subject, "description": request.Description, "status": options.Status, "priority": options.Priority, "severity": options.Severity, "type": options.Type, "assignee": options.Assignee})
			}
			updated, err := target.Client.UpdateIssue(cmd.Context(), target.Issue.ID, request)
			if err != nil {
				return err
			}
			return a.renderIssueMutation("Updated", makeIssueView(updated, target.Project.Slug))
		},
	}
	addEditFlags(command, &options)
	command.ValidArgsFunction = a.completeIssues
	_ = command.RegisterFlagCompletionFunc("status", a.completeIssueStatuses)
	_ = command.RegisterFlagCompletionFunc("priority", a.completeNamedMetadata("issue-priorities", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssuePriorities(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("severity", a.completeNamedMetadata("issue-severities", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssueSeverities(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("type", a.completeNamedMetadata("issue-types", func(ctx context.Context, client *taiga.Client, projectID int64) ([]taiga.NamedMetadata, error) {
		return client.IssueTypes(ctx, projectID)
	}))
	_ = command.RegisterFlagCompletionFunc("assignee", a.completeMembers)
	return command
}

func addEditFlags(command *cobra.Command, options *editIssueOptions) {
	flags := command.Flags()
	flags.StringVar(&options.Subject, "subject", "", "new subject")
	flags.StringVar(&options.Description, "description", "", "new description")
	flags.StringVar(&options.Status, "status", "", "status name")
	flags.StringVar(&options.Priority, "priority", "", "priority name")
	flags.StringVar(&options.Severity, "severity", "", "severity name")
	flags.StringVar(&options.Type, "type", "", "issue type name")
	flags.StringVar(&options.Assignee, "assignee", "", "assignee username or full name")
	flags.IntVar(&options.BaseVersion, "base-version", 0, "explicit Taiga base version")
	flags.BoolVar(&options.DryRun, "dry-run", false, "resolve and display the mutation without writing")
}

func (a *App) issueCloseCommand() *cobra.Command {
	return a.closeWorkItemCommand(closeCommandSpec{
		use:              "close <ref|project#ref|url>",
		short:            "Move an issue to a closed status",
		statusHelp:       "closed status name; required when the project has multiple closed statuses in non-interactive mode",
		dryRunAction:     "close",
		completeItems:    a.completeIssues,
		completeStatuses: a.completeIssueStatuses,
		plan: func(ctx context.Context, argument, status string) (closePlan, error) {
			target, err := a.loadIssueTarget(ctx, argument)
			if err != nil {
				return closePlan{}, err
			}
			selected, err := a.resolveCloseStatus(ctx, target.Client, target.Project.ID, status)
			if err != nil {
				return closePlan{}, err
			}
			return closePlan{client: target.Client, project: target.Project, itemID: target.Issue.ID, ref: target.Issue.Ref, version: target.Issue.Version, statusID: selected.ID, statusName: selected.Name}, nil
		},
		apply: func(ctx context.Context, plan closePlan) error {
			updated, err := plan.client.UpdateIssue(ctx, plan.itemID, taiga.UpdateIssueRequest{Version: plan.version, Status: &plan.statusID})
			if err != nil {
				return err
			}
			return a.renderIssueMutation("Closed", makeIssueView(updated, plan.project.Slug))
		},
	})
}

func (a *App) issueAssignCommand() *cobra.Command {
	var assignee string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use:   "assign <ref|project#ref|url>",
		Short: "Assign an issue",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if strings.TrimSpace(assignee) == "" {
				return usageError("--to is required")
			}
			target, err := a.loadIssueTarget(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			userID, err := a.resolveAssignee(cmd.Context(), target.Client, target.Project.ID, assignee)
			if err != nil {
				return err
			}
			version := target.Issue.Version
			if baseVersion > 0 {
				version = baseVersion
			}
			if dryRun {
				return a.renderDryRun("assign", fmt.Sprintf("%s#%d", target.Project.Slug, target.Issue.Ref), map[string]any{"assignee": assignee, "base_version": version})
			}
			updated, err := target.Client.UpdateIssue(cmd.Context(), target.Issue.ID, taiga.UpdateIssueRequest{Version: version, AssignedTo: &userID})
			if err != nil {
				return err
			}
			return a.renderIssueMutation("Assigned", makeIssueView(updated, target.Project.Slug))
		},
	}
	command.Flags().StringVar(&assignee, "to", "", "assignee username or full name")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeIssues
	_ = command.RegisterFlagCompletionFunc("to", a.completeMembers)
	return command
}

func (a *App) issueCommentCommand() *cobra.Command {
	return a.commentWorkItemCommand(commentCommandSpec{
		use:           "comment <ref|project#ref|url>",
		short:         "Comment on an issue",
		dryRunAction:  "comment",
		body:          commentBody,
		completeItems: a.completeIssues,
		load: func(ctx context.Context, argument string) (commentPlan, error) {
			target, err := a.loadIssueTarget(ctx, argument)
			if err != nil {
				return commentPlan{}, err
			}
			return commentPlan{client: target.Client, project: target.Project, itemID: target.Issue.ID, ref: target.Issue.Ref, version: target.Issue.Version}, nil
		},
		apply: func(ctx context.Context, plan commentPlan, body string) error {
			updated, err := plan.client.UpdateIssue(ctx, plan.itemID, taiga.UpdateIssueRequest{Version: plan.version, Comment: &body})
			if err != nil {
				return err
			}
			return a.renderIssueMutation("Commented on", makeIssueView(updated, plan.project.Slug))
		},
	})
}

func (a *App) loadIssueTarget(ctx context.Context, value string) (issueTarget, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return issueTarget{}, err
	}
	ref, err := taiga.ParseItemRef(value, settings.Project)
	if err != nil {
		return issueTarget{}, validationError("invalid_ref", err.Error())
	}
	project, err := client.GetProjectBySlug(ctx, ref.Project)
	if err != nil {
		return issueTarget{}, err
	}
	issue, err := client.GetIssueByRef(ctx, project.Slug, ref.Ref)
	if err != nil {
		return issueTarget{}, err
	}
	return issueTarget{Client: client, Project: project, Issue: issue, Ref: ref}, nil
}

func makeIssueView(issue taiga.Issue, projectSlug string) issueView {
	assignee := ""
	if issue.AssignedToExtraInfo != nil {
		assignee = firstNonEmpty(issue.AssignedToExtraInfo.Username, issue.AssignedToExtraInfo.FullName)
	}
	return issueView{ID: issue.ID, Ref: issue.Ref, Project: projectSlug, Subject: issue.Subject, Description: issue.Description, Version: issue.Version, Status: issue.StatusExtraInfo.Name, Priority: issue.PriorityExtraInfo.Name, Severity: issue.SeverityExtraInfo.Name, Type: issue.TypeExtraInfo.Name, Assignee: assignee, IsClosed: issue.IsClosed, IsWatcher: issue.IsWatcher, CreatedDate: issue.CreatedDate, ModifiedDate: issue.ModifiedDate}
}

func (a *App) resolveIssueStatus(ctx context.Context, client *taiga.Client, projectID int64, name string, requireClosed bool) (taiga.IssueStatus, error) {
	values, err := client.IssueStatuses(ctx, projectID)
	if err != nil {
		return taiga.IssueStatus{}, err
	}
	matches := []taiga.IssueStatus{}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) && (!requireClosed || value.IsClosed) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.IssueStatus{}, validationError("unknown_status", fmt.Sprintf("issue status %q was not found", name))
	}
	return taiga.IssueStatus{}, validationError("ambiguous_status", fmt.Sprintf("issue status %q matches multiple values", name))
}

func (a *App) resolveCloseStatus(ctx context.Context, client *taiga.Client, projectID int64, name string) (taiga.IssueStatus, error) {
	if name != "" {
		return a.resolveIssueStatus(ctx, client, projectID, name, true)
	}
	values, err := client.IssueStatuses(ctx, projectID)
	if err != nil {
		return taiga.IssueStatus{}, err
	}
	closed := []taiga.IssueStatus{}
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
		return taiga.IssueStatus{}, validationError("missing_closed_status", "project has no closed issue status")
	}
	if a.global.NoInput || !a.stdinTTY() {
		names := make([]string, 0, len(closed))
		for _, value := range closed {
			names = append(names, value.Name)
		}
		return taiga.IssueStatus{}, validationError("ambiguous_status", "multiple closed statuses are available; pass --status: "+strings.Join(names, ", "))
	}
	_, _ = fmt.Fprintln(a.Err, "Closed statuses:")
	for _, value := range closed {
		_, _ = fmt.Fprintf(a.Err, "  %s\n", value.Name)
	}
	selected, err := a.readLine("Status: ")
	if err != nil {
		return taiga.IssueStatus{}, err
	}
	return a.resolveIssueStatus(ctx, client, projectID, selected, true)
}

func (a *App) resolveNamedMetadata(ctx context.Context, name string, loader func(context.Context) ([]taiga.NamedMetadata, error)) (taiga.NamedMetadata, error) {
	values, err := loader(ctx)
	if err != nil {
		return taiga.NamedMetadata{}, err
	}
	matches := []taiga.NamedMetadata{}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.NamedMetadata{}, validationError("unknown_metadata", fmt.Sprintf("metadata value %q was not found", name))
	}
	return taiga.NamedMetadata{}, validationError("ambiguous_metadata", fmt.Sprintf("metadata value %q matches multiple values", name))
}

func (a *App) resolveAssignee(ctx context.Context, client *taiga.Client, projectID int64, name string) (int64, error) {
	users, err := client.ListProjectUsers(ctx, projectID)
	if err != nil {
		return 0, err
	}
	matches := []int64{}
	for _, user := range users {
		if strings.EqualFold(strings.TrimSpace(user.Username), strings.TrimSpace(name)) || strings.EqualFold(strings.TrimSpace(user.FullName), strings.TrimSpace(name)) {
			matches = append(matches, user.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return 0, validationError("unknown_assignee", fmt.Sprintf("project member %q was not found", name))
	}
	return 0, validationError("ambiguous_assignee", fmt.Sprintf("project member %q matches multiple users", name))
}

func (a *App) renderIssueMutation(verb string, view issueView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s issue %s#%d: %s (version %d)\n", verb, view.Project, view.Ref, view.Subject, view.Version)
	}
	return nil
}

func (a *App) renderDryRun(action, target string, changes map[string]any) error {
	plan := map[string]any{"action": action, "target": target, "would_write": true, "performed": false, "changes": changes}
	if a.global.JSON {
		return a.renderer().Plan(plan)
	}
	_, _ = fmt.Fprintf(a.Out, "Dry run: would %s %s\n", action, target)
	return nil
}
