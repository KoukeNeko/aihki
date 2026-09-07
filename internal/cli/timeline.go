package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/aihki/internal/taiga"
	"github.com/spf13/cobra"
)

type timelineView struct {
	ID             int64          `json:"id"`
	Project        string         `json:"project"`
	EventType      string         `json:"event_type"`
	Resource       string         `json:"resource"`
	Action         string         `json:"action"`
	Ref            int            `json:"ref,omitempty"`
	Slug           string         `json:"slug,omitempty"`
	Subject        string         `json:"subject,omitempty"`
	User           string         `json:"user,omitempty"`
	Username       string         `json:"username,omitempty"`
	Comment        string         `json:"comment,omitempty"`
	Changes        map[string]any `json:"changes,omitempty"`
	CommentDeleted bool           `json:"comment_deleted"`
	CommentEdited  bool           `json:"comment_edited"`
	Created        string         `json:"created"`
}

func (a *App) timelineCommand() *cobra.Command {
	var onlyRelevant bool
	var page, limit int
	command := &cobra.Command{
		Use: "timeline", Short: "Show the selected project's cross-resource activity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if page < 1 || limit < 1 || limit > 1000 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			client, project, err := a.selectedProject(cmd.Context())
			if err != nil {
				return err
			}
			entries, pagination, err := client.ProjectTimeline(cmd.Context(), project.ID, onlyRelevant, page, limit)
			if err != nil {
				return err
			}
			views := make([]timelineView, 0, len(entries))
			for _, entry := range entries {
				views = append(views, makeTimelineView(entry, project.Slug))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "DATE\tACTION\tRESOURCE\tITEM\tUSER\tDETAIL")
			for _, event := range views {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", event.Created, event.Action, event.Resource, timelineItem(event), event.User, timelineDetail(event))
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().BoolVar(&onlyRelevant, "only-relevant", true, "exclude low-signal changes and deletions")
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum timeline events to return")
	return command
}

func makeTimelineView(entry taiga.TimelineEntry, projectSlug string) timelineView {
	resource, action := parseTimelineEventType(entry.EventType)
	entity := timelineEntity(entry.Data, resource)
	user := mapValue(entry.Data, "user")
	changes, _ := entry.Data["values_diff"].(map[string]any)
	view := timelineView{
		ID: entry.ID, Project: projectSlug, EventType: entry.EventType, Resource: resource, Action: action,
		Ref: int(numberValue(entity, "ref")), Slug: stringValue(entity, "slug"), Subject: firstNonEmpty(stringValue(entity, "subject"), stringValue(entity, "name")),
		User: firstNonEmpty(stringValue(user, "name"), stringValue(user, "username")), Username: stringValue(user, "username"),
		Comment: stringValue(entry.Data, "comment"), Changes: changes, CommentDeleted: boolValue(entry.Data, "comment_deleted"),
		CommentEdited: boolValue(entry.Data, "comment_edited"), Created: entry.Created,
	}
	return view
}

func parseTimelineEventType(value string) (string, string) {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return value, "event"
	}
	resource := parts[len(parts)-2]
	action := parts[len(parts)-1]
	switch resource {
	case "userstory":
		resource = "story"
	case "wikipage":
		resource = "wiki"
	case "milestone":
		resource = "sprint"
	case "membership":
		resource = "member"
	case "relateduserstory":
		resource = "epic-story"
	}
	return resource, action
}

func timelineEntity(data map[string]any, resource string) map[string]any {
	key := resource
	switch resource {
	case "story":
		key = "userstory"
	case "wiki":
		key = "wikipage"
	case "sprint":
		key = "milestone"
	case "member":
		key = "user"
	case "epic-story":
		key = "userstory"
	}
	return mapValue(data, key)
}

func timelineItem(event timelineView) string {
	if event.Ref > 0 {
		return fmt.Sprintf("#%d %s", event.Ref, event.Subject)
	}
	if event.Slug != "" {
		return event.Slug
	}
	return event.Subject
}

func timelineDetail(event timelineView) string {
	if strings.TrimSpace(event.Comment) != "" {
		return event.Comment
	}
	keys := make([]string, 0, len(event.Changes))
	for key := range event.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func mapValue(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func numberValue(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}

func boolValue(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}
