package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func notifyLevel(value string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "involved", "1":
		return 1, nil
	case "all", "2":
		return 2, nil
	case "none", "3":
		return 3, nil
	default:
		return 0, usageError("notification level must be involved, all, or none")
	}
}

func (a *App) notificationCommand() *cobra.Command {
	command := &cobra.Command{Use: "notification", Short: "Manage notification policies and web notifications"}
	policy := &cobra.Command{Use: "policy", Short: "Manage per-project notification policies"}
	policy.AddCommand(a.notificationPolicyListCommand(), a.notificationPolicyCreateCommand(), a.notificationPolicyEditCommand(), a.notificationPolicyDeleteCommand())
	web := &cobra.Command{Use: "web", Short: "Read and acknowledge web notifications"}
	web.AddCommand(a.webNotificationListCommand(), a.webNotificationReadCommand())
	command.AddCommand(policy, web)
	return command
}

func (a *App) notificationPolicyListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		values, err := client.ListNotifyPolicies(cmd.Context())
		if err != nil {
			return err
		}
		return a.renderer().List(values, map[string]any{"total": len(values)})
	}}
}

func (a *App) notificationPolicyCreateCommand() *cobra.Command {
	var email, live string
	var web, dryRun bool
	command := &cobra.Command{Use: "create", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		notify, err := notifyLevel(email)
		if err != nil {
			return err
		}
		liveLevel, err := notifyLevel(live)
		if err != nil {
			return err
		}
		client, project, err := a.selectedProject(cmd.Context())
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("create notification policy", project.Slug, map[string]any{"notify_level": notify, "live_notify_level": liveLevel, "web_notify_level": web})
		}
		policies, err := client.ListNotifyPolicies(cmd.Context())
		if err != nil {
			return err
		}
		var policyID int64
		for _, policy := range policies {
			if policy.Project == project.ID {
				policyID = policy.ID
				break
			}
		}
		if policyID == 0 {
			return validationError("missing_notification_policy", "Taiga did not provision a notification policy for the selected project membership")
		}
		value, err := client.UpdateNotifyPolicy(cmd.Context(), policyID, map[string]any{"notify_level": notify, "live_notify_level": liveLevel, "web_notify_level": web})
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Created", "notification policy", value)
	}}
	command.Flags().StringVar(&email, "email", "all", "email level: involved, all, or none")
	command.Flags().StringVar(&live, "live", "involved", "live level: involved, all, or none")
	command.Flags().BoolVar(&web, "web", true, "enable web notifications")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) notificationPolicyEditCommand() *cobra.Command {
	var email, live string
	var web, dryRun bool
	command := &cobra.Command{Use: "edit <id>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := positiveID(args[0], "policy")
		if err != nil {
			return err
		}
		fields := map[string]any{}
		if cmd.Flags().Changed("email") {
			level, levelErr := notifyLevel(email)
			if levelErr != nil {
				return levelErr
			}
			fields["notify_level"] = level
		}
		if cmd.Flags().Changed("live") {
			level, levelErr := notifyLevel(live)
			if levelErr != nil {
				return levelErr
			}
			fields["live_notify_level"] = level
		}
		if cmd.Flags().Changed("web") {
			fields["web_notify_level"] = web
		}
		if len(fields) == 0 {
			return usageError("--email, --live, or --web is required")
		}
		if dryRun {
			return a.renderDryRun("edit notification policy", fmt.Sprint(id), fields)
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		value, err := client.UpdateNotifyPolicy(cmd.Context(), id, fields)
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Updated", "notification policy", value)
	}}
	command.Flags().StringVar(&email, "email", "", "email level: involved, all, or none")
	command.Flags().StringVar(&live, "live", "", "live level: involved, all, or none")
	command.Flags().BoolVar(&web, "web", false, "enable or disable web notifications")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) notificationPolicyDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <id>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := positiveID(args[0], "policy")
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete notification policy", fmt.Sprint(id), nil)
		}
		if !yes {
			return confirmationRequired("notification policy deletion requires --yes")
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		if err := client.DeleteNotifyPolicy(cmd.Context(), id); err != nil {
			return err
		}
		return a.renderAdminMutation("Deleted", "notification policy", map[string]any{"id": id, "deleted": true})
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the deletion without writing")
	return command
}

func (a *App) webNotificationListCommand() *cobra.Command {
	var unread bool
	var page, limit int
	command := &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		values, total, err := client.ListWebNotifications(cmd.Context(), unread, page, limit)
		if err != nil {
			return err
		}
		return a.renderer().List(values, map[string]any{"total": total, "page": page, "limit": limit, "only_unread": unread})
	}}
	command.Flags().BoolVar(&unread, "unread", false, "only unread notifications")
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 100, "items per page")
	return command
}

func (a *App) webNotificationReadCommand() *cobra.Command {
	var all, dryRun bool
	command := &cobra.Command{Use: "read [id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if all == (len(args) == 1) {
			return usageError("provide one notification ID or --all")
		}
		target := "all"
		var id int64
		var err error
		if len(args) == 1 {
			id, err = positiveID(args[0], "notification")
			if err != nil {
				return err
			}
			target = fmt.Sprint(id)
		}
		if dryRun {
			return a.renderDryRun("mark web notification read", target, nil)
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		if all {
			err = client.MarkAllWebNotificationsRead(cmd.Context())
		} else {
			err = client.MarkWebNotificationRead(cmd.Context(), id)
		}
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Marked read", "web notification", map[string]any{"target": target, "read": true})
	}}
	command.Flags().BoolVar(&all, "all", false, "mark every notification read")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the mutation without writing")
	return command
}

func (a *App) applicationCommand() *cobra.Command {
	command := &cobra.Command{Use: "application", Short: "Inspect external applications and revoke their tokens"}
	command.AddCommand(a.applicationListCommand(), a.applicationTokenListCommand(), a.applicationTokenRevokeCommand())
	return command
}

func (a *App) applicationListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		values, err := client.ListApplications(cmd.Context())
		if err != nil {
			return err
		}
		return a.renderer().List(values, map[string]any{"total": len(values)})
	}}
}

func (a *App) applicationTokenListCommand() *cobra.Command {
	var application string
	command := &cobra.Command{Use: "tokens", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		var filter *string
		if cmd.Flags().Changed("application") {
			if strings.TrimSpace(application) == "" {
				return usageError("--application cannot be empty")
			}
			filter = &application
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		values, err := client.ListApplicationTokens(cmd.Context(), filter)
		if err != nil {
			return err
		}
		for i := range values {
			values[i].AuthCode = ""
			values[i].NextURL = ""
		}
		return a.renderer().List(values, map[string]any{"total": len(values), "secrets_redacted": true})
	}}
	command.Flags().StringVar(&application, "application", "", "filter by application ID")
	return command
}

func (a *App) applicationTokenRevokeCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "revoke <token-id>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := positiveID(args[0], "token")
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("revoke application token", fmt.Sprint(id), nil)
		}
		if !yes {
			return confirmationRequired("application token revocation requires --yes")
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		if err := client.RevokeApplicationToken(cmd.Context(), id); err != nil {
			return err
		}
		return a.renderAdminMutation("Revoked", "application token", map[string]any{"id": id, "revoked": true})
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm token revocation")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the revocation without writing")
	return command
}

func (a *App) storageCommand() *cobra.Command {
	command := &cobra.Command{Use: "storage", Short: "Manage current-user JSON storage"}
	command.AddCommand(a.storageListCommand(), a.storageGetCommand(), a.storageSetCommand(), a.storageDeleteCommand())
	return command
}

func (a *App) storageListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		values, err := client.ListStorage(cmd.Context())
		if err != nil {
			return err
		}
		return a.renderer().List(values, map[string]any{"total": len(values)})
	}}
}
func (a *App) storageGetCommand() *cobra.Command {
	return &cobra.Command{Use: "get <key>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateStorageKey(args[0]); err != nil {
			return err
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		value, err := client.GetStorage(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Read", "storage entry", value)
	}}
}

func (a *App) storageSetCommand() *cobra.Command {
	var raw string
	var dryRun bool
	command := &cobra.Command{Use: "set <key>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateStorageKey(args[0]); err != nil {
			return err
		}
		if !cmd.Flags().Changed("value") {
			return usageError("--value is required")
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return usageError("--value must be valid JSON: " + err.Error())
		}
		if dryRun {
			return a.renderDryRun("set user storage", args[0], map[string]any{"value": value})
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		_, getErr := client.GetStorage(cmd.Context(), args[0])
		var result taiga.StorageEntry
		if getErr == nil {
			result, err = client.UpdateStorage(cmd.Context(), args[0], value)
		} else {
			var apiErr *taiga.Error
			if !errors.As(getErr, &apiErr) || apiErr.Kind != taiga.KindNotFound {
				return getErr
			}
			result, err = client.CreateStorage(cmd.Context(), args[0], value)
		}
		if err != nil {
			return err
		}
		return a.renderAdminMutation("Set", "storage entry", result)
	}}
	command.Flags().StringVar(&raw, "value", "", "JSON value")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and display without writing")
	return command
}

func (a *App) storageDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <key>", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateStorageKey(args[0]); err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete user storage", args[0], nil)
		}
		if !yes {
			return confirmationRequired("storage deletion requires --yes")
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		if err := client.DeleteStorage(cmd.Context(), args[0]); err != nil {
			return err
		}
		return a.renderAdminMutation("Deleted", "storage entry", map[string]any{"key": args[0], "deleted": true})
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "display the deletion without writing")
	return command
}

func positiveID(value, name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, usageError(name + " ID must be a positive integer")
	}
	return id, nil
}

func validateStorageKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return usageError("storage key cannot be empty")
	}
	if strings.ContainsAny(key, "./") {
		return usageError("storage key cannot contain '.' or '/' because Taiga reserves them for URL routing")
	}
	return nil
}
