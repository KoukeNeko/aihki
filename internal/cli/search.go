package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type searchView struct {
	Kind        string   `json:"kind"`
	ID          int64    `json:"id"`
	Ref         int      `json:"ref,omitempty"`
	Slug        string   `json:"slug,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	StatusID    *int64   `json:"status_id,omitempty"`
	AssignedTo  *int64   `json:"assigned_to,omitempty"`
	TotalPoints *float64 `json:"total_points,omitempty"`
	Sprint      string   `json:"sprint,omitempty"`
	SprintSlug  string   `json:"sprint_slug,omitempty"`
}

func (a *App) searchCommand() *cobra.Command {
	var kind string
	var limit int
	command := &cobra.Command{
		Use: "search <text>", Short: "Search work items in the selected project", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return validationError("empty_search", "search text cannot be empty")
			}
			if limit < 1 || limit > 150 {
				return usageError("--limit must be between 1 and 150")
			}
			kind = normalizeSearchKind(kind)
			if !validSearchKind(kind) {
				return usageError("--type must be all, epic, story, task, issue, or wiki")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			response, err := client.Search(cmd.Context(), project.ID, args[0])
			if err != nil {
				return err
			}
			items := flattenSearch(response, kind, limit)
			if a.global.JSON {
				return a.renderer().List(items, map[string]any{"total": response.Count, "returned": len(items)})
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "TYPE\tREF/SLUG\tSUBJECT\tSPRINT")
			for _, item := range items {
				identifier := item.Slug
				if item.Ref > 0 {
					identifier = fmt.Sprintf("#%d", item.Ref)
				}
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", item.Kind, identifier, item.Subject, item.Sprint)
			}
			return a.flushTable(writer, len(items), response.Count)
		},
	}
	command.Flags().StringVar(&kind, "type", "all", "all, epic, story, task, issue, or wiki")
	command.Flags().IntVar(&limit, "limit", 150, "maximum results to return")
	return command
}

func flattenSearch(response taiga.SearchResponse, kind string, limit int) []searchView {
	items := []searchView{}
	appendItems := func(itemKind string, values []taiga.SearchItem) {
		if kind != "all" && kind != itemKind {
			return
		}
		for _, value := range values {
			if len(items) >= limit {
				return
			}
			items = append(items, searchView{Kind: itemKind, ID: value.ID, Ref: value.Ref, Slug: value.Slug, Subject: value.Subject, StatusID: value.Status, AssignedTo: value.AssignedTo, TotalPoints: value.TotalPoints, Sprint: value.MilestoneName, SprintSlug: value.MilestoneSlug})
		}
	}
	appendItems("epic", response.Epics)
	appendItems("story", response.UserStories)
	appendItems("task", response.Tasks)
	appendItems("issue", response.Issues)
	appendItems("wiki", response.WikiPages)
	return items
}

func normalizeSearchKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "userstory", "user-story", "us":
		return "story"
	case "wikipage", "wiki-page":
		return "wiki"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validSearchKind(value string) bool {
	valid := map[string]struct{}{"all": {}, "epic": {}, "story": {}, "task": {}, "issue": {}, "wiki": {}}
	_, ok := valid[value]
	return ok
}
