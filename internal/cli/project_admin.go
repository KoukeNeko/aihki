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

func (a *App) dueDateCommand() *cobra.Command {
	command := &cobra.Command{Use: "due-date", Short: "Manage project due-date presets"}
	command.AddCommand(a.dueDateListCommand(), a.dueDateCreateCommand(), a.dueDateEditCommand(), a.dueDateDeleteCommand())
	return command
}

func normalizeDueDateResource(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "story", "userstory", "user-story":
		return "story", nil
	case "task":
		return "task", nil
	case "issue":
		return "issue", nil
	default:
		return "", usageError("resource must be story, task, or issue")
	}
}

func (a *App) dueDateListCommand() *cobra.Command {
	return &cobra.Command{Use: "list <story|task|issue>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeDueDateResource(args[0])
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		values, err := client.ListDueDates(cmd.Context(), resource, project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(values, map[string]any{"project": project.Slug, "resource": resource, "total": len(values)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tNAME\tDAYS\tDEFAULT\tCOLOR")
		for _, value := range values {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%d\t%t\t%s\n", value.ID, value.Name, value.DaysToDue, value.ByDefault, value.Color)
		}
		return writer.Flush()
	}}
}

type dueDateOptions struct {
	name, color          string
	days, order          int
	defaultValue, dryRun bool
}

func bindDueDateFlags(command *cobra.Command, options *dueDateOptions) {
	command.Flags().StringVar(&options.name, "name", "", "preset name")
	command.Flags().IntVar(&options.days, "days", 0, "days until due")
	command.Flags().IntVar(&options.order, "order", 0, "display order")
	command.Flags().StringVar(&options.color, "color", "", "color in #RRGGBB format")
	command.Flags().BoolVar(&options.defaultValue, "default", false, "make this the default preset")
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "display the mutation without writing")
}

func dueDateFields(cmd *cobra.Command, options dueDateOptions, create bool) (map[string]any, error) {
	fields := map[string]any{}
	if create || cmd.Flags().Changed("name") {
		if strings.TrimSpace(options.name) == "" {
			return nil, usageError("--name is required and cannot be empty")
		}
		fields["name"] = options.name
	}
	if create || cmd.Flags().Changed("days") {
		if options.days < 0 {
			return nil, usageError("--days cannot be negative")
		}
		fields["days_to_due"] = options.days
	}
	if cmd.Flags().Changed("order") {
		fields["order"] = options.order
	}
	if cmd.Flags().Changed("color") {
		if !metadataColorPattern.MatchString(options.color) {
			return nil, usageError("--color requires #RRGGBB")
		}
		fields["color"] = options.color
	}
	if cmd.Flags().Changed("default") {
		fields["by_default"] = options.defaultValue
	}
	return fields, nil
}

func (a *App) dueDateCreateCommand() *cobra.Command {
	options := dueDateOptions{}
	command := &cobra.Command{Use: "create <story|task|issue>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeDueDateResource(args[0])
		if err != nil {
			return err
		}
		fields, err := dueDateFields(cmd, options, true)
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if options.dryRun {
			return a.renderDryRun("create due-date preset", project.Slug+"#"+resource, fields)
		}
		value, err := client.CreateDueDate(cmd.Context(), resource, project.ID, fields)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Created", resource+" due-date", value)
	}}
	bindDueDateFlags(command, &options)
	return command
}

func (a *App) dueDateEditCommand() *cobra.Command {
	options := dueDateOptions{}
	command := &cobra.Command{Use: "edit <story|task|issue> <id|name>", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeDueDateResource(args[0])
		if err != nil {
			return err
		}
		fields, err := dueDateFields(cmd, options, false)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			return usageError("at least one due-date edit flag is required")
		}
		client, project, current, err := a.resolveDueDate(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		if options.dryRun {
			return a.renderDryRun("edit due-date preset", project.Slug+"#"+current.Name, fields)
		}
		value, err := client.UpdateDueDate(cmd.Context(), resource, current.ID, fields)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Updated", resource+" due-date", value)
	}}
	bindDueDateFlags(command, &options)
	return command
}

func (a *App) dueDateDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <story|task|issue> <id|name>", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeDueDateResource(args[0])
		if err != nil {
			return err
		}
		client, project, current, err := a.resolveDueDate(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		if current.ByDefault {
			return validationError("default_due_date", "Taiga does not allow deleting the default due-date preset")
		}
		if dryRun {
			return a.renderDryRun("delete due-date preset", project.Slug+"#"+current.Name, map[string]any{"id": current.ID, "resource": resource})
		}
		if !yes {
			return confirmationRequired("due-date deletion requires --yes")
		}
		if err := client.DeleteDueDate(cmd.Context(), resource, current.ID); err != nil {
			return err
		}
		return a.renderAdminMutation("Deleted", resource+" due-date", map[string]any{"id": current.ID, "name": current.Name, "deleted": true, "verified": true})
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the deletion without writing")
	return command
}

func (a *App) resolveDueDate(ctx context.Context, resource, selector string) (*taiga.Client, taiga.Project, taiga.DueDate, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.DueDate{}, err
	}
	values, err := client.ListDueDates(ctx, resource, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.DueDate{}, err
	}
	for _, value := range values {
		if fmt.Sprint(value.ID) == strings.TrimSpace(selector) || strings.EqualFold(value.Name, strings.TrimSpace(selector)) {
			return client, project, value, nil
		}
	}
	return nil, taiga.Project{}, taiga.DueDate{}, validationError("unknown_due_date", fmt.Sprintf("due-date preset %q was not found", selector))
}

func (a *App) swimlaneCommand() *cobra.Command {
	command := &cobra.Command{Use: "swimlane", Short: "Manage Kanban swimlanes and per-column WIP limits"}
	command.AddCommand(a.swimlaneListCommand(), a.swimlaneCreateCommand(), a.swimlaneEditCommand(), a.swimlaneDeleteCommand(), a.swimlaneWIPCommand())
	return command
}

func (a *App) swimlaneListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		values, err := client.ListSwimlanes(cmd.Context(), project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(values, map[string]any{"project": project.Slug, "total": len(values)})
		}
		for _, value := range values {
			_, _ = fmt.Fprintf(a.Out, "%d\t%s\t%d\n", value.ID, value.Name, value.Order)
		}
		return nil
	}}
}

func (a *App) swimlaneCreateCommand() *cobra.Command {
	var name string
	var order int
	var dryRun bool
	command := &cobra.Command{Use: "create", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(name) == "" {
			return usageError("--name is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("create swimlane", project.Slug+"#"+name, map[string]any{"name": name, "order": order})
		}
		value, err := client.CreateSwimlane(cmd.Context(), project.ID, name, order)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Created", "swimlane", value)
	}}
	command.Flags().StringVar(&name, "name", "", "swimlane name")
	command.Flags().IntVar(&order, "order", 0, "display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) swimlaneEditCommand() *cobra.Command {
	var name string
	var order int
	var dryRun bool
	command := &cobra.Command{Use: "edit <id|name>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		fields := map[string]any{}
		if cmd.Flags().Changed("name") {
			if strings.TrimSpace(name) == "" {
				return usageError("--name cannot be empty")
			}
			fields["name"] = name
		}
		if cmd.Flags().Changed("order") {
			fields["order"] = order
		}
		if len(fields) == 0 {
			return usageError("--name or --order is required")
		}
		client, project, current, err := a.resolveSwimlane(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("edit swimlane", project.Slug+"#"+current.Name, fields)
		}
		value, err := client.UpdateSwimlane(cmd.Context(), current.ID, fields)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Updated", "swimlane", value)
	}}
	command.Flags().StringVar(&name, "name", "", "swimlane name")
	command.Flags().IntVar(&order, "order", 0, "display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) swimlaneDeleteCommand() *cobra.Command {
	var moveTo string
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <id|name>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, current, err := a.resolveSwimlane(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		var moveID *int64
		if moveTo != "" {
			_, _, target, resolveErr := a.resolveSwimlane(cmd.Context(), moveTo)
			if resolveErr != nil {
				return resolveErr
			}
			if target.ID == current.ID {
				return usageError("--move-to must identify a different swimlane")
			}
			moveID = &target.ID
		}
		if dryRun {
			return a.renderDryRun("delete swimlane", project.Slug+"#"+current.Name, map[string]any{"id": current.ID, "move_to": moveID})
		}
		if !yes {
			return confirmationRequired("swimlane deletion requires --yes")
		}
		if err := client.DeleteSwimlane(cmd.Context(), current.ID, moveID); err != nil {
			return err
		}
		return a.renderAdminMutation("Deleted", "swimlane", map[string]any{"id": current.ID, "name": current.Name, "deleted": true, "verified": true})
	}}
	command.Flags().StringVar(&moveTo, "move-to", "", "replacement swimlane ID or name when required")
	command.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the deletion without writing")
	return command
}

func (a *App) swimlaneWIPCommand() *cobra.Command {
	var limit int
	var clear, dryRun bool
	command := &cobra.Command{Use: "wip <swimlane> <status>", Short: "Set or clear a swimlane column WIP limit", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if clear == cmd.Flags().Changed("limit") {
			return usageError("exactly one of --limit or --clear is required")
		}
		if limit < 0 {
			return usageError("--limit cannot be negative")
		}
		client, project, lane, err := a.resolveSwimlane(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		statuses, err := client.ListWorkflowMetadata(cmd.Context(), "story-status", project.ID)
		if err != nil {
			return err
		}
		statusID := int64(0)
		for _, status := range statuses {
			if fmt.Sprint(status.ID) == args[1] || strings.EqualFold(status.Name, args[1]) || strings.EqualFold(status.Slug, args[1]) {
				statusID = status.ID
				break
			}
		}
		if statusID == 0 {
			return validationError("unknown_status", fmt.Sprintf("story status %q was not found", args[1]))
		}
		var relationshipID int64
		for i := range lane.Statuses {
			if lane.Statuses[i].ID == statusID {
				relationshipID = lane.Statuses[i].SwimlaneUserStoryStatusID
				break
			}
		}
		if relationshipID == 0 {
			return validationError("unknown_swimlane_status", "Taiga did not return the selected swimlane/status relationship")
		}
		var value *int
		if !clear {
			value = &limit
		}
		if dryRun {
			return a.renderDryRun("update swimlane WIP", project.Slug+"#"+lane.Name, map[string]any{"status_id": statusID, "wip_limit": value})
		}
		updated, err := client.UpdateSwimlaneWIP(cmd.Context(), relationshipID, value)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Updated", "swimlane WIP", updated)
	}}
	command.Flags().IntVar(&limit, "limit", 0, "WIP item limit")
	command.Flags().BoolVar(&clear, "clear", false, "clear the WIP limit")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) resolveSwimlane(ctx context.Context, selector string) (*taiga.Client, taiga.Project, taiga.Swimlane, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.Swimlane{}, err
	}
	values, err := client.ListSwimlanes(ctx, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.Swimlane{}, err
	}
	for _, value := range values {
		if fmt.Sprint(value.ID) == strings.TrimSpace(selector) || strings.EqualFold(value.Name, strings.TrimSpace(selector)) {
			return client, project, value, nil
		}
	}
	return nil, taiga.Project{}, taiga.Swimlane{}, validationError("unknown_swimlane", fmt.Sprintf("swimlane %q was not found", selector))
}

func (a *App) tagCommand() *cobra.Command {
	command := &cobra.Command{Use: "tag", Short: "Manage project tags"}
	command.AddCommand(a.tagListCommand(), a.tagCreateCommand(), a.tagEditCommand(), a.tagDeleteCommand(), a.tagMixCommand())
	return command
}

func (a *App) tagListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		values, err := client.ProjectTags(cmd.Context(), project.ID)
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			items = append(items, map[string]any{"name": key, "color": values[key]})
		}
		if a.global.JSON {
			return a.renderer().List(items, map[string]any{"project": project.Slug, "total": len(items)})
		}
		for _, item := range items {
			_, _ = fmt.Fprintf(a.Out, "%s\t%v\n", item["name"], item["color"])
		}
		return nil
	}}
}

func (a *App) tagCreateCommand() *cobra.Command {
	var color string
	var dryRun bool
	command := &cobra.Command{Use: "create <name>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if color != "" && !metadataColorPattern.MatchString(color) {
			return usageError("--color requires #RRGGBB")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("create project tag", project.Slug+"#"+args[0], map[string]any{"color": color})
		}
		if err := client.CreateProjectTag(cmd.Context(), project.ID, args[0], color); err != nil {
			return err
		}
		return a.renderAdminMutation("Created", "tag", map[string]any{"name": strings.ToLower(args[0]), "color": color})
	}}
	command.Flags().StringVar(&color, "color", "", "color in #RRGGBB format")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) tagEditCommand() *cobra.Command {
	var name, color string
	var dryRun bool
	command := &cobra.Command{Use: "edit <name>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("color") {
			return usageError("--name or --color is required")
		}
		if cmd.Flags().Changed("color") && color != "" && !metadataColorPattern.MatchString(color) {
			return usageError("--color requires #RRGGBB or an empty value")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("edit project tag", project.Slug+"#"+args[0], map[string]any{"name": name, "color": color})
		}
		if err := client.EditProjectTag(cmd.Context(), project.ID, args[0], name, color, cmd.Flags().Changed("color")); err != nil {
			return err
		}
		return a.renderAdminMutation("Updated", "tag", map[string]any{"from": args[0], "name": name, "color": color})
	}}
	command.Flags().StringVar(&name, "name", "", "new tag name")
	command.Flags().StringVar(&color, "color", "", "new color, or empty to clear")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) tagDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <name>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete project tag", project.Slug+"#"+args[0], nil)
		}
		if !yes {
			return confirmationRequired("tag deletion requires --yes")
		}
		if err := client.DeleteProjectTag(cmd.Context(), project.ID, args[0]); err != nil {
			return err
		}
		return a.renderAdminMutation("Deleted", "tag", map[string]any{"name": args[0], "deleted": true})
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm deletion from every project work item")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the deletion without writing")
	return command
}

func (a *App) tagMixCommand() *cobra.Command {
	var from []string
	var dryRun bool
	command := &cobra.Command{Use: "mix <target>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(from) == 0 {
			return usageError("at least one --from tag is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("merge project tags", project.Slug+"#"+args[0], map[string]any{"from": from})
		}
		if err := client.MixProjectTags(cmd.Context(), project.ID, from, args[0]); err != nil {
			return err
		}
		return a.renderAdminMutation("Merged", "tags", map[string]any{"from": from, "to": args[0]})
	}}
	command.Flags().StringArrayVar(&from, "from", nil, "source tag to merge (repeatable)")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) renderAdminMutation(verb, kind string, value any) error {
	if a.global.JSON {
		return a.renderer().Data(value)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s %s\n", verb, kind)
	}
	return nil
}
