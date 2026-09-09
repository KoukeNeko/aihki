package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type activityTarget struct {
	Client    *taiga.Client
	Project   string
	ProjectID int64
	ID        int64
	Ref       int
	Slug      string
	Subject   string
	IsWatcher bool
	IsVoter   bool
}

type watchView struct {
	Resource string `json:"resource"`
	Project  string `json:"project"`
	Ref      int    `json:"ref,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Subject  string `json:"subject"`
	Watching bool   `json:"watching"`
	Verified bool   `json:"verified"`
}

type historyView struct {
	ID             string         `json:"id"`
	CreatedAt      string         `json:"created_at"`
	Kind           string         `json:"kind"`
	Author         string         `json:"author"`
	Username       string         `json:"username,omitempty"`
	Comment        string         `json:"comment,omitempty"`
	Changes        map[string]any `json:"changes,omitempty"`
	CommentDeleted bool           `json:"comment_deleted"`
	CommentEdited  bool           `json:"comment_edited"`
	IsHidden       bool           `json:"is_hidden"`
	IsSnapshot     bool           `json:"is_snapshot"`
}

func (a *App) watchCommand(resource string, watching bool) *cobra.Command {
	name, verb, short := "unwatch", "Stopped watching", "Stop watching "+workItemArticle(resource)
	if watching {
		name, verb, short = "watch", "Watching", "Watch "+workItemArticle(resource)
	}
	var dryRun bool
	command := &cobra.Command{
		Use: name + " " + activityArgument(resource), Short: short, Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun(name+" "+resource, target.reference(), map[string]any{"watching": watching})
			}
			verified := target.IsWatcher
			if verified != watching {
				verified, err = target.Client.SetWatching(cmd.Context(), resource, target.ID, watching)
				if err != nil {
					return err
				}
			}
			view := watchView{Resource: resource, Project: target.Project, Ref: target.Ref, Slug: target.Slug, Subject: target.Subject, Watching: verified, Verified: verified == watching}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			if !a.global.Quiet {
				suffix := ""
				if target.Subject != "" {
					suffix = ": " + target.Subject
				}
				_, _ = fmt.Fprintf(a.Out, "%s %s %s%s\n", verb, resource, target.reference(), suffix)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the watch change without writing")
	command.ValidArgsFunction = a.activityCompletion(resource)
	return command
}

func (a *App) historyCommand(resource string) *cobra.Command {
	var historyType string
	var page, limit int
	command := &cobra.Command{
		Use: "history " + activityArgument(resource), Short: "Show " + resource + " activity and comments", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if historyType != "all" && historyType != "activity" && historyType != "comment" {
				return usageError("--type must be all, activity, or comment")
			}
			if page < 1 || limit < 1 || limit > 1000 {
				return usageError("--page must be positive and --limit must be between 1 and 1000")
			}
			target, err := a.loadActivityTarget(cmd.Context(), resource, args[0])
			if err != nil {
				return err
			}
			entries, pagination, err := target.Client.History(cmd.Context(), resource, target.ID, historyType, page, limit)
			if err != nil {
				return err
			}
			views := make([]historyView, 0, len(entries))
			for _, entry := range entries {
				views = append(views, makeHistoryView(entry))
			}
			if a.global.JSON {
				return a.renderer().List(views, pagination)
			}
			writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "DATE\tKIND\tAUTHOR\tDETAIL")
			for _, entry := range views {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", entry.CreatedAt, entry.Kind, entry.Author, historySummary(entry))
			}
			return a.flushTable(writer, len(views), pagination.Total)
		},
	}
	command.Flags().StringVar(&historyType, "type", "all", "history type: all, activity, or comment")
	command.Flags().IntVar(&page, "page", 1, "page number")
	command.Flags().IntVar(&limit, "limit", 30, "maximum history entries to return")
	command.ValidArgsFunction = a.activityCompletion(resource)
	return command
}

func (a *App) loadActivityTarget(ctx context.Context, resource, value string) (activityTarget, error) {
	switch resource {
	case "issue":
		target, err := a.loadIssueTarget(ctx, value)
		if err != nil {
			return activityTarget{}, err
		}
		return activityTarget{Client: target.Client, Project: target.Project.Slug, ProjectID: target.Project.ID, ID: target.Issue.ID, Ref: target.Issue.Ref, Subject: target.Issue.Subject, IsWatcher: target.Issue.IsWatcher, IsVoter: target.Issue.IsVoter}, nil
	case "story":
		target, err := a.loadStoryTarget(ctx, value)
		if err != nil {
			return activityTarget{}, err
		}
		return activityTarget{Client: target.Client, Project: target.Project.Slug, ProjectID: target.Project.ID, ID: target.Story.ID, Ref: target.Story.Ref, Subject: target.Story.Subject, IsWatcher: target.Story.IsWatcher, IsVoter: target.Story.IsVoter}, nil
	case "task":
		target, err := a.loadTaskTarget(ctx, value)
		if err != nil {
			return activityTarget{}, err
		}
		return activityTarget{Client: target.Client, Project: target.Project.Slug, ProjectID: target.Project.ID, ID: target.Task.ID, Ref: target.Task.Ref, Subject: target.Task.Subject, IsWatcher: target.Task.IsWatcher, IsVoter: target.Task.IsVoter}, nil
	case "wiki":
		target, err := a.loadWikiTarget(ctx, value)
		if err != nil {
			return activityTarget{}, err
		}
		return activityTarget{Client: target.Client, Project: target.Project.Slug, ProjectID: target.Project.ID, ID: target.Page.ID, Slug: target.Page.Slug, IsWatcher: target.Page.IsWatcher}, nil
	case "epic":
		target, err := a.loadEpicTarget(ctx, value)
		if err != nil {
			return activityTarget{}, err
		}
		return activityTarget{Client: target.Client, Project: target.Project.Slug, ProjectID: target.Project.ID, ID: target.Epic.ID, Ref: target.Epic.Ref, Subject: target.Epic.Subject, IsWatcher: target.Epic.IsWatcher, IsVoter: target.Epic.IsVoter}, nil
	default:
		return activityTarget{}, usageError("resource must be issue, story, task, wiki, or epic")
	}
}

func activityArgument(resource string) string {
	if resource == "wiki" {
		return "<slug|project#slug|url>"
	}
	return "<ref|project#ref|url>"
}

func (target activityTarget) reference() string {
	if target.Slug != "" {
		return target.Project + "#" + target.Slug
	}
	return fmt.Sprintf("%s#%d", target.Project, target.Ref)
}

func workItemArticle(resource string) string {
	if resource == "issue" || resource == "epic" {
		return "an " + resource
	}
	if resource == "wiki" {
		return "a wiki page"
	}
	return "a " + resource
}

func (a *App) activityCompletion(resource string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	switch resource {
	case "issue":
		return a.completeIssues
	case "story":
		return a.completeStories
	case "wiki":
		return a.completeWikiPages
	case "epic":
		return a.completeEpics
	default:
		return a.completeTasks
	}
}

func makeHistoryView(entry taiga.HistoryEntry) historyView {
	changes := entry.ValuesDiff
	if len(changes) == 0 {
		changes = entry.Diff
	}
	author := firstNonEmpty(entry.User.Name, entry.User.Username, "system")
	return historyView{
		ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: historyKind(entry.Type), Author: author,
		Username: entry.User.Username, Comment: entry.Comment, Changes: changes,
		CommentDeleted: entry.DeleteCommentDate != "", CommentEdited: entry.EditCommentDate != "",
		IsHidden: entry.IsHidden, IsSnapshot: entry.IsSnapshot,
	}
}

func historyKind(value int) string {
	switch value {
	case 1:
		return "change"
	case 2:
		return "create"
	case 3:
		return "delete"
	default:
		return fmt.Sprintf("unknown:%d", value)
	}
}

func historySummary(entry historyView) string {
	if entry.Comment != "" {
		comment := strings.Join(strings.Fields(entry.Comment), " ")
		runes := []rune(comment)
		if len(runes) > 72 {
			comment = string(runes[:69]) + "..."
		}
		if entry.CommentDeleted {
			return "deleted comment"
		}
		return comment
	}
	keys := make([]string, 0, len(entry.Changes))
	for key := range entry.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return entry.Kind
	}
	return strings.Join(keys, ", ")
}
