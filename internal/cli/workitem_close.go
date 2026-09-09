package cli

import (
	"context"
	"fmt"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

// closePlan is everything the shared command needs after the per-type lookups
// have run: which record to write to, at which version, and the closed status
// chosen for it.
type closePlan struct {
	client     *taiga.Client
	project    taiga.Project
	itemID     int64
	ref        int
	version    int
	statusID   int64
	statusName string
}

// closeCommandSpec describes one work item type's move-to-a-closed-status
// command. Issue, story and task differ in the words they print and the calls
// they make, never in the order of what they do: reject a negative version,
// load the record, resolve the closed status, then either rehearse or write.
type closeCommandSpec struct {
	use          string
	short        string
	statusHelp   string
	dryRunAction string

	completeItems    func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)
	completeStatuses func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

	// plan loads the record and resolves the requested status. It runs for a
	// dry run too, so that a rehearsal reports a status that does not exist
	// rather than reporting success for a write that would have failed.
	plan func(ctx context.Context, argument, status string) (closePlan, error)
	// apply writes the change and renders it, which is where the types differ
	// most: each has its own request shape, view and wording.
	apply func(ctx context.Context, plan closePlan) error
}

func (a *App) closeWorkItemCommand(spec closeCommandSpec) *cobra.Command {
	var status string
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
			plan, err := spec.plan(cmd.Context(), args[0], status)
			if err != nil {
				return err
			}
			// An explicit version is the caller saying which state they read,
			// so it wins over the one that was just fetched.
			if baseVersion > 0 {
				plan.version = baseVersion
			}
			if dryRun {
				return a.renderDryRun(spec.dryRunAction, fmt.Sprintf("%s#%d", plan.project.Slug, plan.ref),
					map[string]any{"status": plan.statusName, "base_version": plan.version})
			}
			return spec.apply(cmd.Context(), plan)
		},
	}
	command.Flags().StringVar(&status, "status", "", spec.statusHelp)
	command.Flags().IntVar(&baseVersion, "base-version", 0, "explicit Taiga base version")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = spec.completeItems
	_ = command.RegisterFlagCompletionFunc("status", spec.completeStatuses)
	return command
}
