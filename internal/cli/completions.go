package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KoukeNeko/taiga-cli/internal/completioncache"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

const completionTimeout = 2 * time.Second

func (a *App) completionContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cmd.Context(), completionTimeout)
}

func completionResult(values []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	needle := strings.ToLower(strings.TrimSpace(toComplete))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		candidate := strings.SplitN(value, "\t", 2)[0]
		if needle == "" || strings.HasPrefix(strings.ToLower(candidate), needle) {
			filtered = append(filtered, value)
		}
	}
	sort.Strings(filtered)
	return filtered, cobra.ShellCompDirectiveNoFileComp
}

func noCompletions() ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func (a *App) completeCached(cmd *cobra.Command, kind string, projectScoped bool, toComplete string, loader func(context.Context) ([]string, error)) ([]string, cobra.ShellCompDirective) {
	ctx, cancel := a.completionContext(cmd)
	defer cancel()
	settings, _, settingsErr := a.resolveSettings(ctx)
	project := ""
	if projectScoped {
		project = settings.Project
	}
	key := ""
	var stale []string
	staleOK := false
	if settingsErr == nil && a.CompletionCache != nil && settings.APIURL != "" {
		key = completioncache.Key(settings.Profile, settings.APIURL, project, kind)
		if values, fresh, ok := a.CompletionCache.Get(key); ok {
			if fresh {
				return completionResult(values, toComplete)
			}
			stale, staleOK = values, true
		}
	}
	values, err := loader(ctx)
	if err == nil {
		if key != "" {
			_ = a.CompletionCache.Put(key, values)
		}
		return completionResult(values, toComplete)
	}
	if staleOK {
		return completionResult(stale, toComplete)
	}
	return noCompletions()
}

func (a *App) completeProfiles(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := a.Config.Load()
	if err != nil {
		return noCompletions()
	}
	values := make([]string, 0, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		values = append(values, fmt.Sprintf("%s\t%s", name, profile.APIURL))
	}
	return completionResult(values, toComplete)
}

func (a *App) completeProjects(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "projects", false, toComplete, func(ctx context.Context) ([]string, error) {
		client, _, err := a.client(ctx, true)
		if err != nil {
			return nil, err
		}
		projects, _, err := client.ListProjects(ctx, 1, 100)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(projects))
		for _, project := range projects {
			values = append(values, fmt.Sprintf("%s\t%s", project.Slug, project.Name))
		}
		return values, nil
	})
}

func (a *App) completeProjectTemplates(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "project-templates", false, toComplete, func(ctx context.Context) ([]string, error) {
		client, _, err := a.client(ctx, true)
		if err != nil {
			return nil, err
		}
		templates, err := client.ListProjectTemplates(ctx)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(templates))
		for _, template := range templates {
			values = append(values, fmt.Sprintf("%s\t%s", template.Slug, template.Name))
		}
		return values, nil
	})
}

func (a *App) completeIssues(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "issues", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		items, _, err := client.ListIssues(ctx, project.ID, 1, 100)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, fmt.Sprintf("%d\t%s", item.Ref, item.Subject))
		}
		return values, nil
	})
}

func (a *App) completeStories(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "stories", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		items, _, err := client.ListUserStories(ctx, project.ID, 1, 100, nil, false, "backlog_order")
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, fmt.Sprintf("%d\t%s", item.Ref, item.Subject))
		}
		return values, nil
	})
}

func (a *App) completeTasks(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "tasks", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		items, _, err := client.ListTasks(ctx, project.ID, nil, 1, 100, "us_order")
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, fmt.Sprintf("%d\t%s", item.Ref, item.Subject))
		}
		return values, nil
	})
}

func (a *App) completeWikiPages(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "wiki", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		pages, _, err := client.ListWikiPages(ctx, project.ID, 1, 100)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(pages))
		for _, page := range pages {
			values = append(values, fmt.Sprintf("%s\tversion %d", page.Slug, page.Version))
		}
		return values, nil
	})
}

func (a *App) completeEpics(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "epics", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		epics, _, err := client.ListEpics(ctx, project.ID, 1, 100)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(epics))
		for _, epic := range epics {
			values = append(values, fmt.Sprintf("%d\t%s", epic.Ref, epic.Subject))
		}
		return values, nil
	})
}

func (a *App) completeEpicStatuses(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "epic-statuses", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		statuses, err := client.EpicStatuses(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(statuses))
		for _, status := range statuses {
			values = append(values, fmt.Sprintf("%s\tclosed=%t", status.Name, status.IsClosed))
		}
		return values, nil
	})
}

func (a *App) completeMembers(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "members", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		users, err := client.ListProjectUsers(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(users))
		for _, user := range users {
			values = append(values, fmt.Sprintf("%s\t%s", user.Username, user.FullName))
		}
		return values, nil
	})
}

func (a *App) completeRoles(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "roles", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		roles, err := client.ListRoles(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(roles))
		for _, role := range roles {
			values = append(values, fmt.Sprintf("%s\t%s", role.Slug, role.Name))
		}
		return values, nil
	})
}

func (a *App) completeSprints(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeSprintValues(cmd, toComplete, true)
}

func (a *App) completeSprintNames(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeSprintValues(cmd, toComplete, false)
}

func (a *App) completeSprintValues(cmd *cobra.Command, toComplete string, includeBacklog bool) ([]string, cobra.ShellCompDirective) {
	kind := "sprint-names"
	if includeBacklog {
		kind = "sprints"
	}
	return a.completeCached(cmd, kind, true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		milestones, err := client.Milestones(ctx, project.ID, nil)
		if err != nil {
			return nil, err
		}
		values := []string{}
		if includeBacklog {
			values = append(values, "backlog\tNo sprint")
		}
		for _, milestone := range milestones {
			values = append(values, fmt.Sprintf("%s\t%s", milestone.Slug, milestone.Name))
		}
		return values, nil
	})
}

func (a *App) completeIssueStatuses(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "issue-statuses", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		statuses, err := client.IssueStatuses(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(statuses))
		for _, status := range statuses {
			values = append(values, fmt.Sprintf("%s\tclosed=%t", status.Name, status.IsClosed))
		}
		return values, nil
	})
}

func (a *App) completeStoryStatuses(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "story-statuses", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		statuses, err := client.UserStoryStatuses(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(statuses))
		for _, status := range statuses {
			values = append(values, fmt.Sprintf("%s\tclosed=%t", status.Name, status.IsClosed))
		}
		return values, nil
	})
}

func (a *App) completeTaskStatuses(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return a.completeCached(cmd, "task-statuses", true, toComplete, func(ctx context.Context) ([]string, error) {
		client, project, err := a.selectedProject(ctx)
		if err != nil {
			return nil, err
		}
		statuses, err := client.TaskStatuses(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(statuses))
		for _, status := range statuses {
			values = append(values, fmt.Sprintf("%s\tclosed=%t", status.Name, status.IsClosed))
		}
		return values, nil
	})
}

func (a *App) completeNamedMetadata(kind string, loader func(context.Context, *taiga.Client, int64) ([]taiga.NamedMetadata, error)) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return a.completeCached(cmd, kind, true, toComplete, func(ctx context.Context) ([]string, error) {
			client, project, err := a.selectedProject(ctx)
			if err != nil {
				return nil, err
			}
			items, err := loader(ctx, client, project.ID)
			if err != nil {
				return nil, err
			}
			values := make([]string, 0, len(items))
			for _, item := range items {
				values = append(values, item.Name)
			}
			return values, nil
		})
	}
}
