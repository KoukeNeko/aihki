package config

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrNotGitRepository = errors.New("current directory is not inside a Git repository")

const configSection = "taiga"

type GitLocal struct {
	dir string
}

type LocalValues struct {
	Profile string `json:"profile,omitempty"`
	Project string `json:"project,omitempty"`
}

func NewGitLocal(dir string) *GitLocal { return &GitLocal{dir: dir} }

func (g *GitLocal) Available(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = g.dir
	return cmd.Run() == nil
}

func (g *GitLocal) Load(ctx context.Context) (LocalValues, error) {
	if !g.Available(ctx) {
		return LocalValues{}, ErrNotGitRepository
	}
	profile, err := g.get(ctx, configSection+".profile")
	if err != nil {
		return LocalValues{}, err
	}
	project, err := g.get(ctx, configSection+".project")
	if err != nil {
		return LocalValues{}, err
	}
	return LocalValues{Profile: profile, Project: project}, nil
}

func (g *GitLocal) Set(ctx context.Context, key, value string) error {
	if !g.Available(ctx) {
		return ErrNotGitRepository
	}
	if key != "profile" && key != "project" {
		return fmt.Errorf("unsupported local config key %q", key)
	}
	gitKey := configSection + "." + key
	cmd := exec.CommandContext(ctx, "git", "config", "--local", gitKey, value) // #nosec G204 -- fixed program, key checked against a two-name allowlist above, and the value is its own argument rather than shell text
	cmd.Dir = g.dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write Git-local config: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (g *GitLocal) get(ctx context.Context, key string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--local", "--get", key) // #nosec G204 -- fixed program, and the key is built from a constant section name
	cmd.Dir = g.dir
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read Git-local config %q: %w", key, err)
	}
	return strings.TrimSpace(string(output)), nil
}
