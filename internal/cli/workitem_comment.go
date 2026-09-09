package cli

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

// commentPlan is what the shared comment command needs once the per-type
// lookup has run.
type commentPlan struct {
	client  *taiga.Client
	project taiga.Project
	itemID  int64
	ref     int
	version int
}

// commentCommandSpec describes one work item type's comment command. The three
// types validate the same flags, read the body the same way and rehearse the
// same shape; only the record they load and the request they send differ.
type commentCommandSpec struct {
	use          string
	short        string
	dryRunAction string

	completeItems func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

	// body names the text in this command's errors. Every caller states it,
	// because which code a command returns is part of its contract and should
	// not depend on a field being left out.
	body bodyWording

	load  func(ctx context.Context, argument string) (commentPlan, error)
	apply func(ctx context.Context, plan commentPlan, body string) error
}

func (a *App) commentWorkItemCommand(spec commentCommandSpec) *cobra.Command {
	var body, bodyFile string
	var baseVersion int
	var dryRun bool
	command := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseVersion < 0 {
				return usageError("--base-version cannot be negative")
			}
			if body != "" && bodyFile != "" {
				return usageError("--body and --body-file are mutually exclusive")
			}
			if body == "" && bodyFile == "" {
				return usageError("--body or --body-file is required")
			}
			// Read before loading, so a missing file fails without a request.
			comment, err := readBody(a.In, body, bodyFile, spec.body)
			if err != nil {
				return err
			}
			plan, err := spec.load(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if baseVersion > 0 {
				plan.version = baseVersion
			}
			if dryRun {
				return a.renderDryRun(spec.dryRunAction, fmt.Sprintf("%s#%d", plan.project.Slug, plan.ref),
					map[string]any{"body": comment, "base_version": plan.version})
			}
			return spec.apply(cmd.Context(), plan, comment)
		},
	}
	command.Flags().StringVar(&body, "body", "", "comment body")
	command.Flags().StringVar(&bodyFile, "body-file", "", "read comment from a file, or - for stdin")
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = spec.completeItems
	return command
}
