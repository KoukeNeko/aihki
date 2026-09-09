package taiga

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type ItemRef struct {
	Project string `json:"project"`
	Ref     int    `json:"ref"`
}

func ParseItemRef(value, defaultProject string) (ItemRef, error) {
	return parseTypedItemRef(value, defaultProject, "issue", map[string]struct{}{"issue": {}})
}

func ParseStoryRef(value, defaultProject string) (ItemRef, error) {
	return parseTypedItemRef(value, defaultProject, "story", map[string]struct{}{"us": {}, "story": {}, "userstory": {}, "user-story": {}})
}

func ParseTaskRef(value, defaultProject string) (ItemRef, error) {
	return parseTypedItemRef(value, defaultProject, "task", map[string]struct{}{"task": {}})
}

func ParseEpicRef(value, defaultProject string) (ItemRef, error) {
	return parseTypedItemRef(value, defaultProject, "epic", map[string]struct{}{"epic": {}})
}

// parseTypedItemRef accepts a bare ref, a project#ref pair, or a Taiga web URL.
// label names the work item kind so that failures point at the command the
// caller actually ran rather than at a generic item.
func parseTypedItemRef(value, defaultProject, label string, kinds map[string]struct{}) (ItemRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ItemRef{}, fmt.Errorf("%s reference cannot be empty", label)
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parts := splitPath(parsed.Path)
		for index := 0; index+3 < len(parts); index++ {
			_, kindMatches := kinds[parts[index+2]]
			if parts[index] == "project" && kindMatches {
				ref, err := strconv.Atoi(parts[index+3])
				if err != nil || ref <= 0 {
					return ItemRef{}, fmt.Errorf("invalid %s ref in URL %q", label, value)
				}
				return ItemRef{Project: parts[index+1], Ref: ref}, nil
			}
		}
		return ItemRef{}, fmt.Errorf("URL %q is not a supported Taiga %s URL", value, label)
	}
	if project, rawRef, ok := strings.Cut(value, "#"); ok {
		ref, err := strconv.Atoi(rawRef)
		if strings.TrimSpace(project) == "" || err != nil || ref <= 0 {
			return ItemRef{}, fmt.Errorf("invalid %s reference %q", label, value)
		}
		return ItemRef{Project: strings.TrimSpace(project), Ref: ref}, nil
	}
	ref, err := strconv.Atoi(value)
	if err != nil || ref <= 0 {
		return ItemRef{}, fmt.Errorf("invalid %s reference %q", label, value)
	}
	if strings.TrimSpace(defaultProject) == "" {
		return ItemRef{}, fmt.Errorf("no project selected; run `taiga project use <slug>` or pass --project")
	}
	return ItemRef{Project: strings.TrimSpace(defaultProject), Ref: ref}, nil
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := parts[:0]
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
