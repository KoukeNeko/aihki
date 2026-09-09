package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) roleCommand() *cobra.Command {
	command := &cobra.Command{Use: "role", Short: "Manage project roles and permissions"}
	command.AddCommand(a.roleListCommand(), a.roleCreateCommand(), a.roleEditCommand(), a.roleDeleteCommand())
	return command
}

func (a *App) roleListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List project roles", RunE: func(cmd *cobra.Command, _ []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		roles, err := client.ListRoles(cmd.Context(), project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(roles, map[string]any{"total": len(roles)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tNAME\tSLUG\tCOMPUTABLE\tMEMBERS\tPERMISSIONS")
		for _, role := range roles {
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%t\t%d\t%d\n", role.ID, role.Name, role.Slug, role.Computable, role.MembersCount, len(role.Permissions))
		}
		return writer.Flush()
	}}
}

func (a *App) roleCreateCommand() *cobra.Command {
	var name string
	var permissions []string
	var computable, dryRun bool
	var order int
	command := &cobra.Command{Use: "create", Short: "Create a project role", RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(name) == "" {
			return usageError("--name is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		request := taiga.CreateRoleRequest{Name: name, Project: project.ID, Computable: computable, Permissions: permissions, Order: order}
		if dryRun {
			return a.renderDryRun("create project role", project.Slug, map[string]any{"name": name, "computable": computable, "permissions": permissions, "order": order})
		}
		role, err := client.CreateRole(cmd.Context(), request)
		if err != nil {
			return err
		}
		return a.renderRoleMutation("Created", role)
	}}
	command.Flags().StringVar(&name, "name", "", "role name")
	command.Flags().StringSliceVar(&permissions, "permission", nil, "Taiga permission; repeat or comma-separate")
	command.Flags().BoolVar(&computable, "computable", true, "include this role in Story points")
	command.Flags().IntVar(&order, "order", 10, "role display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the creation without writing")
	return command
}

func (a *App) roleEditCommand() *cobra.Command {
	var name string
	var permissions []string
	var computable bool
	var order int
	var dryRun bool
	command := &cobra.Command{Use: "edit <id|slug|name>", Short: "Edit a project role", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		changed := cmd.Flags().Changed
		if !changed("name") && !changed("permission") && !changed("computable") && !changed("order") {
			return usageError("at least one edit flag is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		role, err := a.resolveRole(cmd.Context(), client, project.ID, args[0])
		if err != nil {
			return err
		}
		request := taiga.UpdateRoleRequest{}
		if changed("name") {
			if strings.TrimSpace(name) == "" {
				return usageError("--name cannot be empty")
			}
			request.Name = &name
		}
		if changed("permission") {
			request.Permissions = &permissions
		}
		if changed("computable") {
			request.Computable = &computable
		}
		if changed("order") {
			request.Order = &order
		}
		if dryRun {
			return a.renderDryRun("edit project role", role.Slug, map[string]any{"name": request.Name, "computable": request.Computable, "permissions": request.Permissions, "order": request.Order})
		}
		updated, err := client.UpdateRole(cmd.Context(), role.ID, request)
		if err != nil {
			return err
		}
		return a.renderRoleMutation("Updated", updated)
	}}
	command.Flags().StringVar(&name, "name", "", "new role name")
	command.Flags().StringSliceVar(&permissions, "permission", nil, "replace permissions; repeat or comma-separate")
	command.Flags().BoolVar(&computable, "computable", true, "include this role in Story points")
	command.Flags().IntVar(&order, "order", 10, "new role display order")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	command.ValidArgsFunction = a.completeRoles
	return command
}

func (a *App) roleDeleteCommand() *cobra.Command {
	var moveTo string
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <id|slug|name>", Short: "Delete a role and optionally move its members", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		role, err := a.resolveRole(cmd.Context(), client, project.ID, args[0])
		if err != nil {
			return err
		}
		var destination *taiga.Role
		if moveTo != "" {
			resolved, err := a.resolveRole(cmd.Context(), client, project.ID, moveTo)
			if err != nil {
				return err
			}
			if resolved.ID == role.ID {
				return usageError("--move-to must name a different role")
			}
			destination = &resolved
		}
		if role.MembersCount > 0 && destination == nil {
			return validationError("role_in_use", "role has members; pass --move-to <role> before deletion")
		}
		if dryRun {
			return a.renderDryRun("delete project role", role.Slug, map[string]any{"members": role.MembersCount, "move_to": moveTo})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("role deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to delete the role: ", role.Slug))
			if err != nil {
				return err
			}
			if answer != role.Slug {
				return confirmationRequired("role deletion was not confirmed")
			}
		}
		var destinationID *int64
		if destination != nil {
			destinationID = &destination.ID
		}
		if err := client.DeleteRole(cmd.Context(), role.ID, destinationID); err != nil {
			return err
		}
		result := map[string]any{"id": role.ID, "slug": role.Slug, "deleted": true, "moved_to": moveTo}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		_, _ = fmt.Fprintf(a.Out, "Deleted role %s\n", role.Name)
		return nil
	}}
	command.Flags().StringVar(&moveTo, "move-to", "", "destination Role ID, slug, or name for existing members")
	command.Flags().BoolVar(&yes, "yes", false, "confirm role deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	command.ValidArgsFunction = a.completeRoles
	_ = command.RegisterFlagCompletionFunc("move-to", a.completeRoles)
	return command
}

func (a *App) renderRoleMutation(verb string, role taiga.Role) error {
	if a.global.JSON {
		return a.renderer().Data(role)
	}
	_, _ = fmt.Fprintf(a.Out, "%s role %s (%s)\n", verb, role.Name, role.Slug)
	return nil
}
