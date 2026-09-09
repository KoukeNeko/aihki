package taiga

import (
	"fmt"
	"net/url"
	"strings"
)

type WikiRef struct {
	Project string `json:"project"`
	Slug    string `json:"slug"`
}

func ParseWikiRef(value, defaultProject string) (WikiRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return WikiRef{}, fmt.Errorf("wiki slug cannot be empty")
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parts := splitPath(parsed.Path)
		for index := 0; index+3 < len(parts); index++ {
			if parts[index] == "project" && parts[index+2] == "wiki" && parts[index+1] != "" && parts[index+3] != "" {
				return WikiRef{Project: parts[index+1], Slug: parts[index+3]}, nil
			}
		}
		return WikiRef{}, fmt.Errorf("URL %q is not a supported Taiga wiki URL", value)
	}
	project, slug, qualified := strings.Cut(value, "#")
	if qualified {
		project, slug = strings.TrimSpace(project), strings.TrimSpace(slug)
		if project == "" || slug == "" || strings.ContainsAny(slug, "/#") {
			return WikiRef{}, fmt.Errorf("invalid wiki reference %q", value)
		}
		return WikiRef{Project: project, Slug: slug}, nil
	}
	if strings.ContainsAny(value, "/#") {
		return WikiRef{}, fmt.Errorf("invalid wiki slug %q", value)
	}
	if strings.TrimSpace(defaultProject) == "" {
		return WikiRef{}, fmt.Errorf("no project selected; run `taiga project use <slug>` or pass --project")
	}
	return WikiRef{Project: strings.TrimSpace(defaultProject), Slug: value}, nil
}
