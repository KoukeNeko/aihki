package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) memberCommand() *cobra.Command {
	command := &cobra.Command{Use: "member", Short: "Manage project members and invitations"}
	command.AddCommand(a.memberListCommand(), a.memberAddCommand(), a.memberEditCommand(), a.memberRemoveCommand())
	return command
}

func (a *App) memberListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List project memberships", RunE: func(cmd *cobra.Command, _ []string) error {
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		members, err := client.ListMemberships(cmd.Context(), project.ID)
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(members, map[string]any{"total": len(members)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "ID\tUSER/EMAIL\tROLE\tADMIN\tOWNER\tACTIVE")
		for _, member := range members {
			identity := firstNonEmpty(member.FullName, member.UserEmail, member.Email)
			_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\t%t\t%t\t%t\n", member.ID, identity, member.RoleName, member.IsAdmin, member.IsOwner, member.IsUserActive)
		}
		return writer.Flush()
	}}
}

func (a *App) memberAddCommand() *cobra.Command {
	var role, invitationText string
	var admin, dryRun bool
	command := &cobra.Command{Use: "add <username|email>", Short: "Add a member or send an invitation", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(role) == "" {
			return usageError("--role is required")
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		selected, err := a.resolveRole(cmd.Context(), client, project.ID, role)
		if err != nil {
			return err
		}
		request := taiga.CreateMembershipRequest{Username: args[0], Project: project.ID, Role: selected.ID, IsAdmin: admin, InvitationText: invitationText}
		if dryRun {
			return a.renderDryRun("add project member", project.Slug, map[string]any{"username": args[0], "role": selected.Name, "admin": admin, "invitation_text": invitationText})
		}
		member, err := client.CreateMembership(cmd.Context(), request)
		if err != nil {
			return err
		}
		return a.renderMemberMutation("Added", member)
	}}
	command.Flags().StringVar(&role, "role", "", "Role ID, slug, or name")
	command.Flags().BoolVar(&admin, "admin", false, "grant project admin privileges")
	command.Flags().StringVar(&invitationText, "invitation-text", "", "extra text for invitation email")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the addition without writing")
	_ = command.RegisterFlagCompletionFunc("role", a.completeRoles)
	return command
}

func (a *App) memberEditCommand() *cobra.Command {
	var role string
	var admin bool
	var dryRun bool
	command := &cobra.Command{Use: "edit <membership-id|email|name>", Short: "Edit a project membership", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("role") && !cmd.Flags().Changed("admin") {
			return usageError("--role or --admin is required")
		}
		client, project, member, err := a.loadMembership(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		request := taiga.UpdateMembershipRequest{}
		if cmd.Flags().Changed("role") {
			selected, err := a.resolveRole(cmd.Context(), client, project.ID, role)
			if err != nil {
				return err
			}
			request.Role = &selected.ID
		}
		if cmd.Flags().Changed("admin") {
			request.IsAdmin = &admin
		}
		if dryRun {
			return a.renderDryRun("edit project member", strconv.FormatInt(member.ID, 10), map[string]any{"role": request.Role, "admin": request.IsAdmin})
		}
		updated, err := client.UpdateMembership(cmd.Context(), member.ID, request)
		if err != nil {
			return err
		}
		return a.renderMemberMutation("Updated", updated)
	}}
	command.Flags().StringVar(&role, "role", "", "new Role ID, slug, or name")
	command.Flags().BoolVar(&admin, "admin", false, "set project admin privileges")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	_ = command.RegisterFlagCompletionFunc("role", a.completeRoles)
	return command
}

func (a *App) memberRemoveCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "remove <membership-id|email|name>", Short: "Remove a member or invitation", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, member, err := a.loadMembership(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		identity := firstNonEmpty(member.FullName, member.UserEmail, member.Email, strconv.FormatInt(member.ID, 10))
		if dryRun {
			return a.renderDryRun("remove project member", project.Slug, map[string]any{"membership_id": member.ID, "identity": identity})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("member removal requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to remove the membership: ", identity))
			if err != nil {
				return err
			}
			if answer != identity {
				return confirmationRequired("member removal was not confirmed")
			}
		}
		if err := client.DeleteMembership(cmd.Context(), member.ID); err != nil {
			return err
		}
		result := map[string]any{"membership_id": member.ID, "identity": identity, "project": project.Slug, "removed": true}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		_, _ = fmt.Fprintf(a.Out, "Removed %s from project %s\n", identity, project.Slug)
		return nil
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm membership removal")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the removal without writing")
	return command
}

func (a *App) loadMembership(ctx context.Context, value string) (*taiga.Client, taiga.Project, taiga.Membership, error) {
	client, project, err := a.selectedProject(ctx)
	if err != nil {
		return nil, taiga.Project{}, taiga.Membership{}, err
	}
	members, err := client.ListMemberships(ctx, project.ID)
	if err != nil {
		return nil, taiga.Project{}, taiga.Membership{}, err
	}
	matches := []taiga.Membership{}
	id, idErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	for _, member := range members {
		if (idErr == nil && member.ID == id) || strings.EqualFold(value, member.Email) || strings.EqualFold(value, member.UserEmail) || strings.EqualFold(value, member.FullName) {
			matches = append(matches, member)
		}
	}
	if len(matches) == 1 {
		return client, project, matches[0], nil
	}
	if len(matches) == 0 {
		return nil, taiga.Project{}, taiga.Membership{}, validationError("unknown_member", fmt.Sprintf("project membership %q was not found", value))
	}
	return nil, taiga.Project{}, taiga.Membership{}, validationError("ambiguous_member", fmt.Sprintf("project membership %q matches multiple entries; use membership ID", value))
}

func (a *App) resolveRole(ctx context.Context, client *taiga.Client, projectID int64, value string) (taiga.Role, error) {
	roles, err := client.ListRoles(ctx, projectID)
	if err != nil {
		return taiga.Role{}, err
	}
	id, idErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	matches := []taiga.Role{}
	for _, role := range roles {
		if (idErr == nil && role.ID == id) || strings.EqualFold(value, role.Slug) || strings.EqualFold(value, role.Name) {
			matches = append(matches, role)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return taiga.Role{}, validationError("unknown_role", fmt.Sprintf("project role %q was not found", value))
	}
	return taiga.Role{}, validationError("ambiguous_role", fmt.Sprintf("project role %q matches multiple values", value))
}

func (a *App) renderMemberMutation(verb string, member taiga.Membership) error {
	if a.global.JSON {
		return a.renderer().Data(member)
	}
	identity := firstNonEmpty(member.FullName, member.UserEmail, member.Email, strconv.FormatInt(member.ID, 10))
	_, _ = fmt.Fprintf(a.Out, "%s membership %s as %s\n", verb, identity, member.RoleName)
	return nil
}
