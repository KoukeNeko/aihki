package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

type sprintView struct {
	ID            int64   `json:"id"`
	Project       string  `json:"project"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	Start         string  `json:"start"`
	Finish        string  `json:"finish"`
	Closed        bool    `json:"closed"`
	Order         int     `json:"order"`
	Disponibility float64 `json:"disponibility"`
	CreatedDate   string  `json:"created_date,omitempty"`
	ModifiedDate  string  `json:"modified_date,omitempty"`
}

func (a *App) sprintCommand() *cobra.Command {
	command := &cobra.Command{Use: "sprint", Aliases: []string{"milestone"}, Short: "Work with Taiga sprints"}
	command.AddCommand(a.sprintListCommand(), a.sprintViewCommand(), a.sprintCreateCommand(), a.sprintEditCommand(), a.sprintStateCommand(true), a.sprintStateCommand(false), a.sprintDeleteCommand())
	return command
}

func (a *App) sprintDeleteCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{Use: "delete <name|slug>", Short: "Permanently delete a Sprint", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, project, sprint, err := a.loadSprint(cmd, args[0])
		if err != nil {
			return err
		}
		if dryRun {
			return a.renderDryRun("delete Sprint", project.Slug+"#"+sprint.Slug, map[string]any{"id": sprint.ID, "name": sprint.Name, "permanent": true})
		}
		if !yes {
			if a.global.NoInput || !a.stdinTTY() {
				return confirmationRequired("Sprint deletion requires --yes in non-interactive mode")
			}
			answer, err := a.readLine(fmt.Sprintf("Type %s to permanently delete the Sprint: ", sprint.Slug))
			if err != nil {
				return err
			}
			if answer != sprint.Slug {
				return confirmationRequired("Sprint deletion was not confirmed")
			}
		}
		if err := client.DeleteMilestone(cmd.Context(), sprint.ID); err != nil {
			return err
		}
		result := map[string]any{"id": sprint.ID, "project": project.Slug, "slug": sprint.Slug, "name": sprint.Name, "deleted": true, "verified": true}
		if a.global.JSON {
			return a.renderer().Data(result)
		}
		if !a.global.Quiet {
			_, _ = fmt.Fprintf(a.Out, "Deleted Sprint %s#%s\n", project.Slug, sprint.Slug)
		}
		return nil
	}}
	command.Flags().BoolVar(&yes, "yes", false, "confirm permanent Sprint deletion")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the deletion without writing")
	command.ValidArgsFunction = a.completeSprintNames
	return command
}

func (a *App) sprintListCommand() *cobra.Command {
	var state, orderBy string
	var page, limit int
	command := &cobra.Command{
		Use: "list", Short: "List sprints in the selected project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > 1000 || page < 1 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			if err := validateSprintOrderBy(orderBy); err != nil {
				return err
			}
			closed, err := parseSprintState(state)
			if err != nil {
				return err
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			milestones, pagination, err := client.ListMilestones(cmd.Context(), project.ID, closed, page, limit, orderBy)
			if err != nil {
				return err
			}
			views := make([]sprintView, 0, len(milestones))
			for _, milestone := range milestones {
				views = append(views, makeSprintView(milestone, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "SLUG\tNAME\tSTART\tFINISH\tCLOSED")
			for _, sprint := range views {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%t\n", sprint.Slug, sprint.Name, sprint.Start, sprint.Finish, sprint.Closed)
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().StringVar(&state, "state", "open", "open, closed, or all")
	command.Flags().StringVar(&orderBy, "order-by", "-estimated_start", "order field, prefix with - for descending")
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum sprints to return")
	return command
}

func (a *App) sprintViewCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "view <name|slug>", Short: "View a sprint", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, milestone, err := a.loadSprint(cmd, args[0])
			if err != nil {
				return err
			}
			fresh, err := client.GetMilestone(cmd.Context(), milestone.ID)
			if err != nil {
				return err
			}
			view := makeSprintView(fresh, project.Slug)
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "%s\nSlug:    %s\nProject: %s\nStart:   %s\nFinish:  %s\nClosed:  %t\n", view.Name, view.Slug, view.Project, view.Start, view.Finish, view.Closed)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeSprintNames
	return command
}

func (a *App) sprintCreateCommand() *cobra.Command {
	var name, start, finish string
	var dryRun bool
	command := &cobra.Command{
		Use: "create", Short: "Create a sprint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" || start == "" || finish == "" {
				return usageError("--name, --start, and --finish are required")
			}
			if err := validateSprintDates(start, finish); err != nil {
				return err
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			request := taiga.CreateMilestoneRequest{Project: project.ID, Name: name, EstimatedStart: start, EstimatedFinish: finish}
			if dryRun {
				return a.renderDryRun("create sprint", project.Slug, map[string]any{"name": name, "start": start, "finish": finish})
			}
			milestone, err := client.CreateMilestone(cmd.Context(), request)
			if err != nil {
				return err
			}
			return a.renderSprintMutation("Created", makeSprintView(milestone, project.Slug))
		},
	}
	command.Flags().StringVar(&name, "name", "", "sprint name")
	command.Flags().StringVar(&start, "start", "", "estimated start date (YYYY-MM-DD)")
	command.Flags().StringVar(&finish, "finish", "", "estimated finish date (YYYY-MM-DD)")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	return command
}

func (a *App) sprintEditCommand() *cobra.Command {
	var name, start, finish string
	var dryRun bool
	command := &cobra.Command{
		Use: "edit <name|slug>", Short: "Edit a sprint", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("start") && !cmd.Flags().Changed("finish") {
				return usageError("at least one edit flag is required")
			}
			client, project, milestone, err := a.loadSprint(cmd, args[0])
			if err != nil {
				return err
			}
			request := taiga.UpdateMilestoneRequest{}
			if cmd.Flags().Changed("name") {
				if strings.TrimSpace(name) == "" {
					return validationError("empty_name", "sprint name cannot be empty")
				}
				request.Name = &name
			}
			if cmd.Flags().Changed("start") {
				request.EstimatedStart = &start
			}
			if cmd.Flags().Changed("finish") {
				request.EstimatedFinish = &finish
			}
			effectiveStart, effectiveFinish := milestone.EstimatedStart, milestone.EstimatedFinish
			if request.EstimatedStart != nil {
				effectiveStart = *request.EstimatedStart
			}
			if request.EstimatedFinish != nil {
				effectiveFinish = *request.EstimatedFinish
			}
			if err := validateSprintDates(effectiveStart, effectiveFinish); err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun("edit sprint", milestone.Slug, map[string]any{"name": request.Name, "start": request.EstimatedStart, "finish": request.EstimatedFinish})
			}
			updated, err := client.UpdateMilestone(cmd.Context(), milestone.ID, request)
			if err != nil {
				return err
			}
			return a.renderSprintMutation("Updated", makeSprintView(updated, project.Slug))
		},
	}
	command.Flags().StringVar(&name, "name", "", "new sprint name")
	command.Flags().StringVar(&start, "start", "", "new estimated start date (YYYY-MM-DD)")
	command.Flags().StringVar(&finish, "finish", "", "new estimated finish date (YYYY-MM-DD)")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeSprintNames
	return command
}

func (a *App) sprintStateCommand(closed bool) *cobra.Command {
	name, past, short := "close", "Closed", "Close a sprint"
	if !closed {
		name, past, short = "reopen", "Reopened", "Reopen a sprint"
	}
	var dryRun bool
	command := &cobra.Command{
		Use: name + " <name|slug>", Short: short, Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, milestone, err := a.loadSprint(cmd, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun(name+" sprint", milestone.Slug, map[string]any{"closed": closed})
			}
			updated, err := client.UpdateMilestone(cmd.Context(), milestone.ID, taiga.UpdateMilestoneRequest{Closed: &closed})
			if err != nil {
				return err
			}
			return a.renderSprintMutation(past, makeSprintView(updated, project.Slug))
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the mutation without writing")
	command.ValidArgsFunction = a.completeSprintNames
	return command
}

func (a *App) loadSprint(cmd *cobra.Command, value string) (*taiga.Client, taiga.Project, taiga.Milestone, error) {
	client, project, err := a.selectedProject(cmd.Context())
	if err != nil {
		return nil, taiga.Project{}, taiga.Milestone{}, err
	}
	milestone, err := a.resolveMilestone(cmd.Context(), client, project.ID, value)
	return client, project, milestone, err
}

func makeSprintView(milestone taiga.Milestone, projectSlug string) sprintView {
	return sprintView{ID: milestone.ID, Project: projectSlug, Name: milestone.Name, Slug: milestone.Slug, Start: milestone.EstimatedStart, Finish: milestone.EstimatedFinish, Closed: milestone.Closed, Order: milestone.Order, Disponibility: milestone.Disponibility, CreatedDate: milestone.CreatedDate, ModifiedDate: milestone.ModifiedDate}
}

func parseSprintState(value string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "open":
		closed := false
		return &closed, nil
	case "closed":
		closed := true
		return &closed, nil
	case "all":
		return nil, nil
	default:
		return nil, usageError("--state must be open, closed, or all")
	}
}

func validateSprintOrderBy(value string) error {
	field := strings.TrimPrefix(strings.TrimSpace(value), "-")
	allowed := map[string]struct{}{"project": {}, "name": {}, "estimated_start": {}, "estimated_finish": {}, "closed": {}, "created_date": {}}
	if _, ok := allowed[field]; !ok {
		return usageError(fmt.Sprintf("unsupported sprint order field %q", value))
	}
	return nil
}

func validateSprintDates(start, finish string) error {
	startDate, err := time.Parse("2006-01-02", start)
	if err != nil {
		return validationError("invalid_start_date", "sprint start must use YYYY-MM-DD")
	}
	finishDate, err := time.Parse("2006-01-02", finish)
	if err != nil {
		return validationError("invalid_finish_date", "sprint finish must use YYYY-MM-DD")
	}
	if startDate.After(finishDate) {
		return validationError("invalid_date_range", "sprint start must not be after finish")
	}
	return nil
}

func (a *App) renderSprintMutation(verb string, view sprintView) error {
	if a.global.JSON {
		return a.renderer().Data(view)
	}
	if !a.global.Quiet {
		_, _ = fmt.Fprintf(a.Out, "%s sprint %s: %s to %s\n", verb, view.Slug, view.Start, view.Finish)
	}
	return nil
}
