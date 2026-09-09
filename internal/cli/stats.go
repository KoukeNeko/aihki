package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type projectStatsView struct {
	Project string `json:"project"`
	taiga.ProjectStats
}

type projectIssueStatsView struct {
	Project string `json:"project"`
	taiga.ProjectIssueStats
}

type sprintStatsView struct {
	Project string `json:"project"`
	Slug    string `json:"slug"`
	taiga.SprintStats
}

type memberStatsView struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name,omitempty"`
	ClosedBugs   int    `json:"closed_bugs"`
	CreatedBugs  int    `json:"created_bugs"`
	ClosedTasks  int    `json:"closed_tasks"`
	WikiChanges  int    `json:"wiki_changes"`
	IocaineTasks int    `json:"iocaine_tasks"`
}

func (a *App) statsCommand() *cobra.Command {
	command := &cobra.Command{Use: "stats", Short: "Show project, sprint, and Taiga statistics"}
	command.AddCommand(a.projectStatsCommand(), a.issueStatsCommand(), a.memberStatsCommand(), a.sprintStatsCommand(), a.systemStatsCommand(), a.discoverStatsCommand())
	return command
}

func (a *App) projectStatsCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "project [slug]", Short: "Show project backlog and velocity statistics", Args: maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, err := a.statsProject(cmd.Context(), args)
			if err != nil {
				return err
			}
			stats, err := client.GetProjectStats(cmd.Context(), project.ID)
			if err != nil {
				return err
			}
			view := projectStatsView{Project: project.Slug, ProjectStats: stats}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "%s (%s)\nDefined points: %.2f\nAssigned points: %.2f\nClosed points: %.2f\nVelocity: %.2f\n", stats.Name, project.Slug, stats.DefinedPoints, stats.AssignedPoints, stats.ClosedPoints, stats.Speed)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) issueStatsCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "issues [project-slug]", Short: "Show issue totals, breakdowns, and four-week trends", Args: maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, err := a.statsProject(cmd.Context(), args)
			if err != nil {
				return err
			}
			stats, err := client.GetProjectIssueStats(cmd.Context(), project.ID)
			if err != nil {
				return err
			}
			view := projectIssueStatsView{Project: project.Slug, ProjectIssueStats: stats}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "%s issues\nTotal: %d\nOpen: %d\nClosed: %d\n", project.Slug, stats.TotalIssues, stats.OpenedIssues, stats.ClosedIssues)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) memberStatsCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "members [project-slug]", Short: "Show per-member contribution counters", Args: maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, err := a.statsProject(cmd.Context(), args)
			if err != nil {
				return err
			}
			stats, err := client.GetProjectMemberStats(cmd.Context(), project.ID)
			if err != nil {
				return err
			}
			users, err := client.ListProjectUsers(cmd.Context(), project.ID)
			if err != nil {
				return err
			}
			views := makeMemberStatsViews(users, stats)
			if a.global.JSON {
				return a.renderer().List(views, map[string]any{"project": project.Slug, "total": len(views)})
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "USER\tCREATED ISSUES\tCLOSED ISSUES\tCLOSED TASKS\tWIKI CHANGES")
			for _, member := range views {
				_, _ = fmt.Fprintf(writer, "%s\t%d\t%d\t%d\t%d\n", member.Username, member.CreatedBugs, member.ClosedBugs, member.ClosedTasks, member.WikiChanges)
			}
			return writer.Flush()
		},
	}
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) sprintStatsCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "sprint <name|slug>", Short: "Show sprint completion and burndown statistics", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, project, sprint, err := a.loadSprint(cmd, args[0])
			if err != nil {
				return err
			}
			stats, err := client.GetSprintStats(cmd.Context(), sprint.ID)
			if err != nil {
				return err
			}
			view := sprintStatsView{Project: project.Slug, Slug: sprint.Slug, SprintStats: stats}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			_, _ = fmt.Fprintf(a.Out, "%s (%s)\nStories: %d/%d complete\nTasks: %d/%d complete\n", stats.Name, sprint.Slug, stats.CompletedUserStories, stats.TotalUserStories, stats.CompletedTasks, stats.TotalTasks)
			return nil
		},
	}
	command.ValidArgsFunction = a.completeSprintNames
	return command
}

func (a *App) systemStatsCommand() *cobra.Command {
	return &cobra.Command{Use: "system", Short: "Show public Taiga instance statistics", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), false)
		if err != nil {
			return err
		}
		stats, err := client.GetSystemStats(cmd.Context())
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(stats)
		}
		_, _ = fmt.Fprintf(a.Out, "Users: %d\nProjects: %d\nUser stories: %d\n", stats.Users.Total, stats.Projects.Total, stats.UserStories.Total)
		return nil
	}}
}

func (a *App) discoverStatsCommand() *cobra.Command {
	return &cobra.Command{Use: "discover", Short: "Show the number of publicly discoverable projects", Args: exactArgs(0), RunE: func(cmd *cobra.Command, _ []string) error {
		client, _, err := a.client(cmd.Context(), false)
		if err != nil {
			return err
		}
		stats, err := client.GetDiscoverStats(cmd.Context())
		if err != nil {
			return err
		}
		if a.global.JSON {
			return a.renderer().Data(stats)
		}
		_, _ = fmt.Fprintf(a.Out, "Discoverable projects: %d\n", stats.Projects.Total)
		return nil
	}}
}

func (a *App) statsProject(ctx context.Context, args []string) (*taiga.Client, taiga.Project, error) {
	client, settings, err := a.client(ctx, true)
	if err != nil {
		return nil, taiga.Project{}, err
	}
	slug := settings.Project
	if len(args) == 1 {
		slug = args[0]
	}
	if slug == "" {
		return nil, taiga.Project{}, validationError("missing_project", "no project selected; run `taiga project use <slug>` or pass a project slug")
	}
	project, err := client.GetProjectBySlug(ctx, slug)
	return client, project, err
}

func makeMemberStatsViews(users []taiga.User, stats taiga.ProjectMemberStats) []memberStatsView {
	byID := map[int64]taiga.User{}
	keys := map[string]struct{}{}
	for _, user := range users {
		byID[user.ID] = user
		keys[strconv.FormatInt(user.ID, 10)] = struct{}{}
	}
	for _, values := range []map[string]int{stats.ClosedBugs, stats.CreatedBugs, stats.ClosedTasks, stats.WikiChanges, stats.IocaineTasks} {
		for key := range values {
			keys[key] = struct{}{}
		}
	}
	views := make([]memberStatsView, 0, len(keys))
	for key := range keys {
		id, _ := strconv.ParseInt(key, 10, 64)
		user := byID[id]
		username := user.Username
		if username == "" {
			username = "unassigned"
		}
		views = append(views, memberStatsView{ID: id, Username: username, Name: user.FullName, ClosedBugs: stats.ClosedBugs[key], CreatedBugs: stats.CreatedBugs[key], ClosedTasks: stats.ClosedTasks[key], WikiChanges: stats.WikiChanges[key], IocaineTasks: stats.IocaineTasks[key]})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Username < views[j].Username })
	return views
}

func maximumArgs(maximum int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > maximum {
			return usageError(fmt.Sprintf("%s accepts at most %d argument(s), received %d%s", cmd.CommandPath(), maximum, len(args), argumentUsage(cmd)))
		}
		return nil
	}
}
