package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) deleteWorkItemCommand(resource string) *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{
		Use: "delete " + activityArgument(resource), Short: "Permanently delete " + workItemArticle(resource), Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun("delete "+resource, target.reference(), map[string]any{"id": target.ID, "subject": target.Subject, "permanent": true})
			}
			if !yes {
				if a.global.NoInput || !a.stdinTTY() {
					return confirmationRequired(resource + " deletion requires --yes in non-interactive mode")
				}
				answer, err := a.readLine(fmt.Sprintf("Type %s to permanently delete the %s: ", target.reference(), resource))
				if err != nil {
					return err
				}
				if answer != target.reference() {
					return confirmationRequired(resource + " deletion was not confirmed")
				}
			}
			if err := target.Client.DeleteWorkItem(cmd.Context(), resource, target.ID); err != nil {
				return err
			}
			result := map[string]any{"resource": resource, "project": target.Project, "ref": target.Ref, "id": target.ID, "subject": target.Subject, "deleted": true, "verified": true}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Deleted %s %s: %s\n", resource, target.reference(), target.Subject)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "confirm permanent deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	command.ValidArgsFunction = a.activityCompletion(resource)
	return command
}

func (a *App) participantCommand(resource, kind string) *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use: kind + " " + activityArgument(resource), Short: "List " + resource + " " + kind, Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if page < 1 || limit < 1 || limit > 1000 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[0])
			if err != nil {
				return err
			}
			participants, pagination, err := target.Client.ListParticipants(cmd.Context(), resource, target.ID, kind, page, limit)
			if err != nil {
				return err
			}
			if a.global.JSON {
				return a.renderer().List(participants, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "ID\tUSERNAME\tNAME")
			for _, participant := range participants {
				_, _ = fmt.Fprintf(writer, "%d\t%s\t%s\n", participant.ID, participant.Username, participant.FullName)
			}
			return a.flushTable(writer, len(participants), pagination.Total)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum participants to return")
	command.ValidArgsFunction = a.activityCompletion(resource)
	return command
}

func (a *App) commentCommand() *cobra.Command {
	command := &cobra.Command{Use: "comment", Short: "Edit or delete work item comments by history entry ID"}
	command.AddCommand(a.commentEditCommand(), a.commentDeleteCommand(), a.commentUndeleteCommand(), a.commentVersionsCommand())
	return command
}

func (a *App) commentUndeleteCommand() *cobra.Command {
	var dryRun bool
	command := &cobra.Command{Use: "undelete <epic|story|task|issue|wiki> <ref|slug|url> <history-id>", Short: "Restore a deleted comment", Args: exactArgs(3), ValidArgs: []string{"epic", "story", "task", "issue", "wiki"}, RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeActivityResource(args[0])
		if err != nil {
			return err
		}
		target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		entry, err := target.Client.FindHistoryEntry(cmd.Context(), resource, target.ID, args[2])
		if err != nil {
			return err
		}
		if entry.Comment == "" {
			return validationError("not_comment", "history entry does not contain a comment")
		}
		if entry.DeleteCommentDate == "" {
			return a.renderCommentMutation("Restored", resource, target.reference(), entry)
		}
		if dryRun {
			return a.renderDryRun("restore comment", target.reference(), map[string]any{"history_id": entry.ID, "comment": entry.Comment})
		}
		restored, err := target.Client.UndeleteComment(cmd.Context(), resource, target.ID, entry.ID)
		if err != nil {
			return err
		}
		return a.renderCommentMutation("Restored", resource, target.reference(), restored)
	}}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the restore without writing")
	return command
}

func (a *App) commentVersionsCommand() *cobra.Command {
	return &cobra.Command{Use: "versions <epic|story|task|issue|wiki> <ref|slug|url> <history-id>", Short: "List previous versions of an edited comment", Args: exactArgs(3), ValidArgs: []string{"epic", "story", "task", "issue", "wiki"}, RunE: func(cmd *cobra.Command, args []string) error {
		resource, err := normalizeActivityResource(args[0])
		if err != nil {
			return err
		}
		target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
		if err != nil {
			return err
		}
		versions, err := target.Client.CommentVersions(cmd.Context(), resource, target.ID, args[2])
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().List(versions, map[string]any{"resource": resource, "reference": target.reference(), "history_id": args[2], "total": len(versions)})
		}
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "DATE\tCOMMENT")
		for _, version := range versions {
			_, _ = fmt.Fprintf(writer, "%s\t%s\n", version.Date, version.Comment)
		}
		return writer.Flush()
	}}
}

func (a *App) commentEditCommand() *cobra.Command {
	var body, bodyFile string
	var dryRun bool
	command := &cobra.Command{
		Use: "edit <epic|story|task|issue|wiki> <ref|slug|url> <history-id>", Short: "Edit an existing comment", Args: exactArgs(3), ValidArgs: []string{"epic", "story", "task", "issue", "wiki"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if body != "" && bodyFile != "" {
				return usageError("--body and --body-file are mutually exclusive")
			}
			resource, err := normalizeActivityResource(args[0])
			if err != nil {
				return err
			}
			comment, err := readBody(a.In, body, bodyFile, genericBody)
			if err != nil {
				return err
			}
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
			if err != nil {
				return err
			}
			entry, err := target.Client.FindHistoryEntry(cmd.Context(), resource, target.ID, args[2])
			if err != nil {
				return err
			}
			if entry.Comment == "" {
				return validationError("not_comment", "history entry does not contain a comment")
			}
			if dryRun {
				return a.renderDryRun("edit comment", target.reference(), map[string]any{"history_id": entry.ID, "body": comment})
			}
			updated, err := target.Client.EditComment(cmd.Context(), resource, target.ID, entry.ID, comment)
			if err != nil {
				return err
			}
			return a.renderCommentMutation("Edited", resource, target.reference(), updated)
		},
	}
	command.Flags().StringVar(&body, "body", "", "new comment body")
	command.Flags().StringVar(&bodyFile, "body-file", "", "read the new comment from a file, or - for stdin")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the edit without writing")
	return command
}

func (a *App) commentDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{
		Use: "delete <epic|story|task|issue|wiki> <ref|slug|url> <history-id>", Short: "Delete an existing comment", Args: exactArgs(3), ValidArgs: []string{"epic", "story", "task", "issue", "wiki"},
		RunE: func(cmd *cobra.Command, args []string) error {
			resource, err := normalizeActivityResource(args[0])
			if err != nil {
				return err
			}
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[1])
			if err != nil {
				return err
			}
			entry, err := target.Client.FindHistoryEntry(cmd.Context(), resource, target.ID, args[2])
			if err != nil {
				return err
			}
			if entry.Comment == "" {
				return validationError("not_comment", "history entry does not contain a comment")
			}
			if entry.DeleteCommentDate != "" {
				return a.renderCommentMutation("Deleted", resource, target.reference(), entry)
			}
			if dryRun {
				return a.renderDryRun("delete comment", target.reference(), map[string]any{"history_id": entry.ID, "comment": entry.Comment})
			}
			if !yes {
				if a.global.NoInput || !a.stdinTTY() {
					return confirmationRequired("comment deletion requires --yes in non-interactive mode")
				}
				answer, err := a.readLine(fmt.Sprintf("Type %s to delete the comment: ", entry.ID))
				if err != nil {
					return err
				}
				if answer != entry.ID {
					return confirmationRequired("comment deletion was not confirmed")
				}
			}
			deleted, err := target.Client.DeleteComment(cmd.Context(), resource, target.ID, entry.ID)
			if err != nil {
				return err
			}
			return a.renderCommentMutation("Deleted", resource, target.reference(), deleted)
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "confirm comment deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	return command
}

func normalizeActivityResource(value string) (string, error) {
	resource := strings.ToLower(strings.TrimSpace(value))
	switch resource {
	case "epic", "story", "task", "issue", "wiki":
		return resource, nil
	default:
		return "", usageError("resource must be epic, story, task, issue, or wiki")
	}
}

func (a *App) renderCommentMutation(verb, resource, reference string, entry taiga.HistoryEntry) error {
	view := makeHistoryView(entry)
	result := map[string]any{"resource": resource, "reference": reference, "history": view, "verified": true}
	if a.global.JSON {
		return a.renderer().Data(result)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s comment %s on %s %s\n", verb, entry.ID, resource, reference)
	}
	return nil
}
