package cli

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

var metadataColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type metadataOptions struct {
	Name          string
	Color         string
	Order         int
	Closed        bool
	Archived      bool
	WIPLimit      int
	ClearWIPLimit bool
	Value         float64
	UnsetValue    bool
	DryRun        bool
}

func (a *App) metadataCommand() *cobra.Command {
	command := &cobra.Command{Use: "metadata", Aliases: []string{"workflow-metadata"}, Short: "Manage project workflow metadata"}
	command.AddCommand(a.metadataListCommand(), a.metadataViewCommand(), a.metadataCreateCommand(), a.metadataEditCommand(), a.metadataDeleteCommand())
	return command
}

func metadataKinds() []string {
	return []string{"epic-status", "story-status", "task-status", "issue-status", "points", "priority", "severity", "issue-type"}
}

func (a *App) metadataListCommand() *cobra.Command {
	return &cobra.Command{Use: "list <kind>", Short: "List workflow metadata", Args: exactArgs(1), ValidArgs: metadataKinds(), RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := normalizeMetadataKind(args[0])
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		values, err := client.ListWorkflowMetadata(cmd.Context(), kind, project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(values, map[string]any{"project": project.Slug, "kind": kind, "total": len(values)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tNAME\tORDER\tCOLOR\tCLOSED\tVALUE")
		for _, value := range values {
			points := ""
			if value.Value != nil {
				points = fmt.Sprintf("%g", *value.Value)
			}
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%d\t%s\t%t\t%s\n", value.ID, value.Name, value.Order, value.Color, value.IsClosed, points)
		}
		return writer.Flush()
	}}
}

func (a *App) metadataViewCommand() *cobra.Command {
	return &cobra.Command{Use: "view <kind> <id|name|slug>", Short: "View workflow metadata", Args: exactArgs(2), ValidArgs: metadataKinds(), RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := normalizeMetadataKind(args[0])
		if err != nil {
			return err
		}
		_, _, value, err := a.resolveWorkflowMetadata(cmd.Context(), kind, args[1])
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(value)
		}
		_, _ = fmt.Fprintf(a.Out, "%s\nID: %d\nKind: %s\nOrder: %d\nColor: %s\nClosed: %t\n", value.Name, value.ID, kind, value.Order, value.Color, value.IsClosed)
		return nil
	}}
}

func (a *App) metadataCreateCommand() *cobra.Command {
	options := metadataOptions{}
	command := &cobra.Command{Use: "create <kind>", Short: "Create workflow metadata", Args: exactArgs(1), ValidArgs: metadataKinds(), RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := normalizeMetadataKind(args[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(options.Name) == "" {
			return usageError("--name is required")
		}
		fields, err := metadataFields(cmd, kind, options, true)
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if options.DryRun {
			return a.renderDryRun("create workflow metadata", project.Slug+"#"+kind, fields)
		}
		created, err := client.CreateWorkflowMetadata(cmd.Context(), kind, project.ID, fields)
		if err != nil {
			return err
		}
		return a.renderMetadataMutation("Created", kind, project.Slug, created)
	}}
	bindMetadataFlags(command, &options)
	return command
}

func (a *App) metadataEditCommand() *cobra.Command {
	options := metadataOptions{}
	command := &cobra.Command{Use: "edit <kind> <id|name|slug>", Short: "Edit workflow metadata", Args: exactArgs(2), ValidArgs: metadataKinds(), RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := normalizeMetadataKind(args[0])
		if err != nil {
			return err
		}
		fields, err := metadataFields(cmd, kind, options, false)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			return usageError("at least one metadata edit flag is required")
		}
		client, project, current, err := a.resolveWorkflowMetadata(cmd.Context(), kind, args[1])
		if err != nil {
			return err
		}
		if options.DryRun {
			return a.renderDryRun("edit workflow metadata", project.Slug+"#"+current.Name, fields)
		}
		updated, err := client.UpdateWorkflowMetadata(cmd.Context(), kind, current.ID, fields)
		if err != nil {
			return err
		}
		return a.renderMetadataMutation("Updated", kind, project.Slug, updated)
	}}
	bindMetadataFlags(command, &options)
	return command
}

func (a *App) metadataDeleteCommand() *cobra.Command {
	var moveTo string
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <kind> <id|name|slug>", Short: "Delete workflow metadata and move related items", Args: exactArgs(2), ValidArgs: metadataKinds(), RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := normalizeMetadataKind(args[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(moveTo) == "" {
			return usageError("--move-to is required")
		}
		client, project, current, err := a.resolveWorkflowMetadata(cmd.Context(), kind, args[1])
		if err != nil {
			return err
		}
		_, _, replacement, err := a.resolveWorkflowMetadata(cmd.Context(), kind, moveTo)
		if err != nil {
			return err
		}
		if current.ID == replacement.ID {
			return usageError("--move-to must identify different metadata")
		}
		if dryRun {
			return a.renderDryRun("delete workflow metadata", project.Slug+"#"+current.Name, map[string]any{"kind": kind, "id": current.ID, "move_to": replacement.Name, "move_to_id": replacement.ID})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("metadata deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to delete the metadata and move related items: ", current.Name))
			if err != nil {
				return err
			}
			if answer != current.Name {
				return confirmationRequired("metadata deletion was not confirmed")
			}
		}
		if err := client.DeleteWorkflowMetadata(cmd.Context(), kind, current.ID, replacement.ID); err != nil {
			return err
		}
		result := map[string]any{"kind": kind, "project": project.Slug, "id": current.ID, "name": current.Name, "deleted": true, "move_to": replacement.Name, "move_to_id": replacement.ID, "verified": true}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		if !a.global.Quiet {
			_, _ = fmt.Fprintf(a.Out, "Deleted %s %s; moved related items to %s\n", kind, current.Name, replacement.Name)
		}
		return nil
	}}
	command.Flags().StringVar(&moveTo, "move-to", "", "replacement metadata ID, name, or slug")
	command.Flags().BoolVar(&yes, "yes", false, "confirm metadata deletion and item migration")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	return command
}

func bindMetadataFlags(command *cobra.Command, options *metadataOptions) {
	command.Flags().StringVar(&options.Name, "name", "", "metadata name")
	command.Flags().StringVar(&options.Color, "color", "", "color in #RRGGBB format")
	command.Flags().IntVar(&options.Order, "order", 0, "display order")
	command.Flags().BoolVar(&options.Closed, "closed", false, "set status closed state")
	command.Flags().BoolVar(&options.Archived, "archived", false, "set Story status archived state")
	command.Flags().IntVar(&options.WIPLimit, "wip-limit", 0, "Story status work-in-progress limit")
	command.Flags().BoolVar(&options.ClearWIPLimit, "clear-wip-limit", false, "clear Story status WIP limit")
	command.Flags().Float64Var(&options.Value, "value", 0, "Points numeric value")
	command.Flags().BoolVar(&options.UnsetValue, "unset-value", false, "set Points value to null")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "resolve and display the mutation without writing")
}

func metadataFields(command *cobra.Command, kind string, options metadataOptions, creating bool) (map[string]any, error) {
	changed := command.Flags().Changed
	if options.ClearWIPLimit && changed("wip-limit") {
		return nil, usageError("--wip-limit and --clear-wip-limit are mutually exclusive")
	}
	if options.UnsetValue && changed("value") {
		return nil, usageError("--value and --unset-value are mutually exclusive")
	}
	status := strings.HasSuffix(kind, "-status")
	storyStatus := kind == "story-status"
	points := kind == "points"
	if (changed("closed") && !status) || ((changed("archived") || changed("wip-limit") || options.ClearWIPLimit) && !storyStatus) || ((changed("value") || options.UnsetValue) && !points) {
		return nil, usageError("one or more flags are not valid for metadata kind " + kind)
	}
	if changed("color") && (points || options.Color == "" || !metadataColorPattern.MatchString(options.Color)) {
		return nil, usageError("--color requires #RRGGBB and is not valid for Points")
	}
	if changed("wip-limit") && options.WIPLimit < 0 {
		return nil, usageError("--wip-limit cannot be negative")
	}
	if changed("value") && options.Value < 0 {
		return nil, usageError("--value cannot be negative")
	}
	fields := map[string]any{}
	if creating || changed("name") {
		if strings.TrimSpace(options.Name) == "" {
			return nil, usageError("--name cannot be empty")
		}
		fields["name"] = options.Name
	}
	if changed("color") {
		fields["color"] = options.Color
	}
	if changed("order") {
		fields["order"] = options.Order
	}
	if changed("closed") {
		fields["is_closed"] = options.Closed
	}
	if changed("archived") {
		fields["is_archived"] = options.Archived
	}
	if changed("wip-limit") {
		fields["wip_limit"] = options.WIPLimit
	} else if options.ClearWIPLimit {
		fields["wip_limit"] = nil
	}
	if changed("value") {
		fields["value"] = options.Value
	} else if options.UnsetValue {
		fields["value"] = nil
	}
	return fields, nil
}

func normalizeMetadataKind(value string) (string, error) {
	kind, err := taiga.NormalizeMetadataKind(value)
	if err != nil {
		return "", usageError(err.Error())
	}
	return kind, nil
}

func (a *App) resolveWorkflowMetadata(ctx context.Context, kind, value string) (*taiga.Client, taiga.Project, taiga.WorkflowMetadata, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.WorkflowMetadata{}, err
	}
	values, err := client.ListWorkflowMetadata(ctx, kind, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.WorkflowMetadata{}, err
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		for _, item := range values {
			if item.ID == id {
				return client, project, item, nil
			}
		}
	}
	matches := []taiga.WorkflowMetadata{}
	for _, item := range values {
		if strings.EqualFold(strings.TrimSpace(value), item.Name) || (item.Slug != "" && strings.EqualFold(strings.TrimSpace(value), item.Slug)) {
			matches = append(matches, item)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	if len(matches) == 1 {
		return client, project, matches[0], nil
	}
	if len(matches) == 0 {
		return nil, taiga.Project{}, taiga.WorkflowMetadata{}, validationError("unknown_metadata", fmt.Sprintf("%s %q was not found", kind, value))
	}
	return nil, taiga.Project{}, taiga.WorkflowMetadata{}, validationError("ambiguous_metadata", fmt.Sprintf("%s %q matches multiple values", kind, value))
}

func (a *App) renderMetadataMutation(verb, kind, project string, value taiga.WorkflowMetadata) error {
	if a.global.JSON {
		return a.renderer().Data(value)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s %s %s#%s (%d)\n", verb, kind, project, value.Name, value.ID)
	}
	return nil
}
