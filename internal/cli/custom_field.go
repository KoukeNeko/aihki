package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

var customFieldTypes = map[string]struct{}{
	"text": {}, "multiline": {}, "richtext": {}, "date": {}, "url": {}, "dropdown": {}, "checkbox": {}, "number": {},
}

type customFieldValuesView struct {
	Resource string         `json:"resource"`
	Project  string         `json:"project"`
	Ref      int            `json:"ref"`
	Values   map[string]any `json:"values"`
	Version  int            `json:"version"`
}

func (a *App) customFieldCommand() *cobra.Command {
	command := &cobra.Command{Use: "custom-field", Aliases: []string{"field"}, Short: "Manage custom field definitions and values"}
	command.AddCommand(a.customFieldListCommand(), a.customFieldCreateCommand(), a.customFieldEditCommand(), a.customFieldDeleteCommand(), a.customFieldValuesCommand(), a.customFieldSetCommand())
	return command
}

func (a *App) customFieldListCommand() *cobra.Command {
	return &cobra.Command{Use: "list <epic|story|task|issue>", Short: "List custom field definitions", Args: exactArgs(1), ValidArgs: []string{"epic", "story", "task", "issue"}, RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		fields, err := client.ListCustomFields(cmd.Context(), resource, project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(fields, map[string]any{"resource": resource, "total": len(fields)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tNAME\tTYPE\tORDER\tDESCRIPTION")
		for _, field := range fields {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%d\t%s\n", field.ID, field.Name, field.Type, field.Order, field.Description)
		}
		return writer.Flush()
	}}
}

func (a *App) customFieldCreateCommand() *cobra.Command {
	var name, description, fieldType string
	var options []string
	var order int64
	var dryRun bool
	command := &cobra.Command{Use: "create <epic|story|task|issue>", Short: "Create a custom field definition", Args: exactArgs(1), ValidArgs: []string{"epic", "story", "task", "issue"}, RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(name) == "" || !validCustomFieldType(fieldType) {
			return usageError("--name and a valid --type are required")
		}
		if fieldType != "dropdown" && len(options) > 0 {
			return usageError("--option is only valid for dropdown fields")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		request := taiga.CreateCustomFieldRequest{Name: name, Description: description, Type: fieldType, Order: order, Project: project.ID}
		if fieldType == "dropdown" {
			request.Extra = map[string]any{"choices": options}
		}
		if dryRun {
			return a.renderDryRun("create custom field", resource, map[string]any{"name": name, "description": description, "type": fieldType, "order": order, "options": options})
		}
		field, err := client.CreateCustomField(cmd.Context(), resource, request)
		if err != nil {
			return err
		}
		return a.renderCustomFieldMutation("Created", resource, field)
	}}
	command.Flags().StringVar(&name, "name", "", "field name")
	command.Flags().StringVar(&description, "description", "", "field description")
	command.Flags().StringVar(&fieldType, "type", "", "text, multiline, richtext, date, url, dropdown, checkbox, or number")
	command.Flags().StringSliceVar(&options, "option", nil, "dropdown choice; repeat or comma-separate")
	command.Flags().Int64Var(&order, "order", 0, "field display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	return command
}

func (a *App) customFieldEditCommand() *cobra.Command {
	var name, description, fieldType string
	var options []string
	var order int64
	var dryRun bool
	command := &cobra.Command{Use: "edit <epic|story|task|issue> <id|name>", Short: "Edit a custom field definition", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		changed := cmd.Flags().Changed
		if !changed("name") && !changed("description") && !changed("type") && !changed("option") && !changed("order") {
			return usageError("at least one edit flag is required")
		}
		if changed("type") && !validCustomFieldType(fieldType) {
			return usageError("--type is invalid")
		}
		client, project, field, err := a.loadCustomField(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		request := taiga.UpdateCustomFieldRequest{}
		if changed("name") {
			if strings.TrimSpace(name) == "" {
				return usageError("--name cannot be empty")
			}
			request.Name = &name
		}
		if changed("description") {
			request.Description = &description
		}
		if changed("type") {
			request.Type = &fieldType
		}
		if changed("order") {
			request.Order = &order
		}
		if changed("option") {
			extra := map[string]any{"choices": options}
			request.Extra = &extra
		}
		if dryRun {
			return a.renderDryRun("edit custom field", strconv.FormatInt(field.ID, 10), map[string]any{"project": project.Slug, "resource": resource, "name": request.Name, "description": request.Description, "type": request.Type, "order": request.Order, "options": options})
		}
		updated, err := client.UpdateCustomField(cmd.Context(), resource, field.ID, request)
		if err != nil {
			return err
		}
		return a.renderCustomFieldMutation("Updated", resource, updated)
	}}
	command.Flags().StringVar(&name, "name", "", "new field name")
	command.Flags().StringVar(&description, "description", "", "new field description")
	command.Flags().StringVar(&fieldType, "type", "", "new field type")
	command.Flags().StringSliceVar(&options, "option", nil, "replace dropdown choices")
	command.Flags().Int64Var(&order, "order", 0, "new display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	return command
}

func (a *App) customFieldDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <epic|story|task|issue> <id|name>", Short: "Delete a custom field definition", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		client, _, field, err := a.loadCustomField(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete custom field", strconv.FormatInt(field.ID, 10), map[string]any{"resource": resource, "name": field.Name})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("custom field deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to delete the custom field: ", field.Name))
			if err != nil {
				return err
			}
			if answer != field.Name {
				return confirmationRequired("custom field deletion was not confirmed")
			}
		}
		if err := client.DeleteCustomField(cmd.Context(), resource, field.ID); err != nil {
			return err
		}
		result := map[string]any{"id": field.ID, "resource": resource, "name": field.Name, "deleted": true}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		_, _ = fmt.Fprintf(a.Out, "Deleted %s custom field %s\n", resource, field.Name)
		return nil
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm custom field deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	return command
}

func (a *App) customFieldValuesCommand() *cobra.Command {
	return &cobra.Command{Use: "values <epic|story|task|issue> <ref>", Short: "View resolved custom field values", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		view, err := a.loadCustomFieldValuesView(cmd.Context(), target, resource)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(view)
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "FIELD\tVALUE")
		for name, value := range view.Values {
			_, _ = fmt.Fprintf(writer, "%s\t%v\n", name, value)
		}
		return writer.Flush()
	}}
}

func (a *App) customFieldSetCommand() *cobra.Command {
	var assignments, unset []string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{Use: "set <epic|story|task|issue> <ref>", Short: "Merge custom field values with OCC", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeCustomFieldResource(args[0])
		if err != nil {
			return err
		}
		if len(assignments) == 0 && len(unset) == 0 {
			return usageError("--value or --unset is required")
		}
		target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		fields, err := target.Client.ListCustomFields(cmd.Context(), resource, target.ProjectID)
		if err != nil {
			return err
		}
		byName := map[string]taiga.CustomField{}
		for _, field := range fields {
			byName[strings.ToLower(field.Name)] = field
		}
		current, err := target.Client.GetCustomFieldValues(cmd.Context(), resource, target.ID)
		if err != nil {
			return err
		}
		merged := map[string]any{}
		for key, value := range current.AttributesValues {
			merged[key] = value
		}
		for _, assignment := range assignments {
			name, raw, ok := strings.Cut(assignment, "=")
			field, found := byName[strings.ToLower(strings.TrimSpace(name))]
			if !ok || !found {
				return validationError("unknown_custom_field", fmt.Sprintf("custom field assignment %q is invalid or unknown", assignment))
			}
			merged[strconv.FormatInt(field.ID, 10)] = parseCustomFieldValue(raw)
		}
		for _, name := range unset {
			field, found := byName[strings.ToLower(strings.TrimSpace(name))]
			if !found {
				return validationError("unknown_custom_field", fmt.Sprintf("custom field %q was not found", name))
			}
			key := strconv.FormatInt(field.ID, 10)
			delete(merged, key)
			// Taiga 6 rejects an empty attributes_values object as a blank
			// model field. Null is its accepted representation for clearing
			// the final remaining custom field value.
			if len(merged) == 0 {
				merged[key] = nil
			}
		}
		version := current.Version
		if baseVersion > 0 {
			version = baseVersion
		}
		if dryRun {
			return a.renderDryRun("set custom field values", target.reference(), map[string]any{"resource": resource, "base_version": version, "values": merged})
		}
		updated, err := target.Client.UpdateCustomFieldValues(cmd.Context(), resource, target.ID, taiga.UpdateCustomFieldValuesRequest{AttributesValues: merged, Version: version})
		if err != nil {
			return err
		}
		view := makeCustomFieldValuesView(target, resource, fields, updated)
		if a.global.JSON {
			return a.renderer().Data(view)
		}
		_, _ = fmt.Fprintf(a.Out, "Updated %s custom field values for %s (version %d)\n", resource, target.reference(), view.Version)
		return nil
	}}
	command.Flags().StringArrayVar(&assignments, "value", nil, "field=value; repeat for multiple fields; value accepts JSON or plain text")
	command.Flags().StringArrayVar(&unset, "unset", nil, "remove a field value; repeat for multiple fields")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit custom values base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the merge without writing")
	return command
}

func normalizeCustomFieldResource(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "epic", "epics":
		return "epic", nil
	case "story", "stories", "userstory", "userstories", "us":
		return "story", nil
	case "task", "tasks":
		return "task", nil
	case "issue", "issues":
		return "issue", nil
	default:
		return "", usageError("resource must be epic, story, task, or issue")
	}
}

func validCustomFieldType(value string) bool {
	_, ok := customFieldTypes[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func (a *App) loadCustomField(ctx context.Context, resource, value string) (*taiga.Client, taiga.Project, taiga.CustomField, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.CustomField{}, err
	}
	fields, err := client.ListCustomFields(ctx, resource, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.CustomField{}, err
	}
	id, idErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	matches := []taiga.CustomField{}
	for _, field := range fields {
		if (idErr == nil && field.ID == id) || strings.EqualFold(value, field.Name) {
			matches = append(matches, field)
		}
	}
	if len(matches) == 1 {
		return client, project, matches[0], nil
	}
	if len(matches) == 0 {
		return nil, taiga.Project{}, taiga.CustomField{}, validationError("unknown_custom_field", fmt.Sprintf("%s custom field %q was not found", resource, value))
	}
	return nil, taiga.Project{}, taiga.CustomField{}, validationError("ambiguous_custom_field", fmt.Sprintf("custom field %q matches multiple values; use ID", value))
}

func (a *App) loadCustomFieldValuesView(ctx context.Context, target activityTarget, resource string) (customFieldValuesView, error) {
	fields, err := target.Client.ListCustomFields(ctx, resource, target.ProjectID)
	if err != nil {
		return customFieldValuesView{}, err
	}
	values, err := target.Client.GetCustomFieldValues(ctx, resource, target.ID)
	if err != nil {
		return customFieldValuesView{}, err
	}
	return makeCustomFieldValuesView(target, resource, fields, values), nil
}

func makeCustomFieldValuesView(target activityTarget, resource string, fields []taiga.CustomField, values taiga.CustomFieldValues) customFieldValuesView {
	names := map[string]string{}
	for _, field := range fields {
		names[strconv.FormatInt(field.ID, 10)] = field.Name
	}
	resolved := map[string]any{}
	for id, value := range values.AttributesValues {
		name := names[id]
		if name == "" {
			name = "#" + id
		}
		resolved[name] = value
	}
	return customFieldValuesView{Resource: resource, Project: target.Project, Ref: target.Ref, Values: resolved, Version: values.Version}
}

func parseCustomFieldValue(raw string) any {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return value
	}
	return raw
}

func (a *App) renderCustomFieldMutation(verb, resource string, field taiga.CustomField) error {
	if a.global.JSON {
		return a.renderer().Data(field)
	}
	_, _ = fmt.Fprintf(a.Out, "%s %s custom field %s (%d)\n", verb, resource, field.Name, field.ID)
	return nil
}
