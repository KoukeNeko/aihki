package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/KoukeNeko/taiga-cli/internal/config"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) configCommand() *cobra.Command {
	var local bool
	command := &cobra.Command{Use: "config", Short: "Manage non-secret configuration"}
	command.PersistentFlags().BoolVar(&local, "local", false, "read or write Git-local profile/project mapping")
	command.AddCommand(a.configGetCommand(&local), a.configSetCommand(&local), a.configListCommand(&local))
	return command
}

func (a *App) configGetCommand(local *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Read a configuration value",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := normalizeConfigKey(args[0])
			var value string
			if *local {
				values, err := a.GitLocal.Load(cmd.Context())
				if err != nil {
					if errors.Is(err, config.ErrNotGitRepository) {
						return validationError("not_git_repository", err.Error())
					}
					return err
				}
				switch key {
				case "profile":
					value = values.Profile
				case "project":
					value = values.Project
				default:
					return usageError("Git-local config supports only profile and project")
				}
			} else {
				settings, _, err := a.resolveSettings(cmd.Context())
				if err != nil {
					return err
				}
				switch key {
				case "profile":
					value = settings.Profile
				case "api-url":
					value = settings.APIURL
				case "project":
					value = settings.Project
				default:
					return usageError("config key must be profile, api-url, or project")
				}
			}
			result := map[string]any{"key": key, "value": value, "local": *local}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			_, _ = fmt.Fprintln(a.Out, value)
			return nil
		},
	}
}

func (a *App) configSetCommand(local *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a configuration value",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := normalizeConfigKey(args[0]), args[1]
			if *local {
				if key != "profile" && key != "project" {
					return usageError("Git-local config supports only profile and project")
				}
				if err := a.GitLocal.Set(cmd.Context(), key, value); err != nil {
					if errors.Is(err, config.ErrNotGitRepository) {
						return validationError("not_git_repository", err.Error())
					}
					return err
				}
			} else {
				settings, cfg, err := a.resolveSettings(cmd.Context())
				if err != nil {
					return err
				}
				switch key {
				case "profile":
					name, err := config.NormalizeProfileName(value)
					if err != nil {
						return validationError("invalid_profile", err.Error())
					}
					cfg.CurrentProfile = name
					ensureProfile(&cfg, name)
				case "api-url":
					normalized, err := taiga.NormalizeAPIURL(value)
					if err != nil {
						return validationError("invalid_api_url", err.Error())
					}
					updateProfile(&cfg, settings.Profile, func(profile *config.Profile) { profile.APIURL = normalized })
				case "project":
					updateProfile(&cfg, settings.Profile, func(profile *config.Profile) { profile.Project = value })
				default:
					return usageError("config key must be profile, api-url, or project")
				}
				if err := a.Config.Save(cfg); err != nil {
					return err
				}
			}
			result := map[string]any{"key": key, "value": value, "local": *local}
			if a.global.JSON {
				return a.renderer().Data(result)
			}
			if !a.global.Quiet {
				_, _ = fmt.Fprintf(a.Out, "Set %s=%s\n", key, value)
			}
			return nil
		},
	}
}

func (a *App) configListCommand(local *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List non-secret configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if *local {
				values, err := a.GitLocal.Load(cmd.Context())
				if err != nil {
					if errors.Is(err, config.ErrNotGitRepository) {
						return validationError("not_git_repository", err.Error())
					}
					return err
				}
				if a.global.JSON {
					return a.renderer().Data(values)
				}
				_, _ = fmt.Fprintf(a.Out, "profile\t%s\nproject\t%s\n", values.Profile, values.Project)
				return nil
			}
			cfg, err := a.Config.Load()
			if err != nil {
				return err
			}
			if a.global.JSON {
				return a.renderer().Data(cfg)
			}
			_, _ = fmt.Fprintf(a.Out, "current-profile\t%s\n", cfg.CurrentProfile)
			names := make([]string, 0, len(cfg.Profiles))
			for name := range cfg.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				profile := cfg.Profiles[name]
				_, _ = fmt.Fprintf(a.Out, "%s\t%s\t%s\n", name, profile.APIURL, profile.Project)
			}
			return nil
		},
	}
}

func normalizeConfigKey(key string) string {
	switch key {
	case "current-profile", "current_profile":
		return "profile"
	case "api_url", "apiurl":
		return "api-url"
	default:
		return key
	}
}
