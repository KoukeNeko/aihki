package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

func (a *App) integrationCommand() *cobra.Command {
	command := &cobra.Command{Use: "integration", Aliases: []string{"importer"}, Short: "Use Taiga third-party project importers"}
	command.AddCommand(a.integrationProvidersCommand(), a.integrationCallCommand())
	return command
}

func (a *App) integrationProvidersCommand() *cobra.Command {
	return &cobra.Command{Use: "providers", Args: exactArgs(0), RunE: func(_ *cobra.Command, _ []string) error {
		items := []map[string]any{
			{"name": "github", "upstream_default": true}, {"name": "jira", "upstream_default": true},
			{"name": "trello", "upstream_default": true}, {"name": "asana", "upstream_default": true},
			{"name": "pivotal", "upstream_default": false}, {"name": "gitlab", "upstream_default": false},
		}
		return a.renderer().List(items, map[string]any{"note": "availability depends on the Taiga server configuration"})
	}}
}

func (a *App) integrationCallCommand() *cobra.Command {
	var fields []string
	var inputFile string
	var yes, dryRun bool
	command := &cobra.Command{Use: "call <provider> <auth-url|authorize|list-projects|list-users|import-project>", Args: exactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		provider, err := taiga.NormalizeImporterProvider(args[0])
		if err != nil {
			return usageError(err.Error())
		}
		action := strings.ToLower(args[1])
		switch action {
		case "auth-url", "authorize", "list-projects", "list-users", "import-project":
		default:
			return usageError("unsupported importer action")
		}
		payload, err := importerFields(fields, inputFile)
		if err != nil {
			return err
		}
		redacted := redactImporterFields(payload)
		if dryRun {
			return a.renderDryRun("call "+provider+" importer "+action, provider, redacted)
		}
		if action == "import-project" && !yes {
			return confirmationRequired("third-party project import requires --yes")
		}
		client, _, err := a.client(cmd.Context(), true)
		if err != nil {
			return err
		}
		result, err := client.ImporterCall(cmd.Context(), provider, action, payload)
		if err != nil {
			return err
		}
		if action == "authorize" {
			result = redactImporterResult(result)
		}
		if a.global.JSON {
			return a.renderer().Data(map[string]any{"provider": provider, "action": action, "result": result, "accepted": action == "import-project"})
		}
		if !a.global.Quiet {
			_, _ = fmt.Fprintf(a.Out, "%s importer %s completed\n", provider, action)
		}
		return nil
	}}
	command.Flags().StringArrayVar(&fields, "field", nil, "request field as key=JSON (repeatable; do not use for secrets)")
	command.Flags().StringVar(&inputFile, "input", "", "JSON object file containing request fields, including credentials")
	command.Flags().BoolVar(&yes, "yes", false, "confirm project import")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and show a redacted request without calling Taiga")
	return command
}

func importerFields(fields []string, inputFile string) (map[string]any, error) {
	result := map[string]any{}
	if inputFile != "" {
		data, err := os.ReadFile(inputFile)
		if err != nil {
			return nil, fmt.Errorf("read importer input: %w", err)
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, usageError("--input must contain a JSON object: " + err.Error())
		}
	}
	for _, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, usageError("--field must use key=JSON")
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			value = raw
		}
		result[strings.TrimSpace(key)] = value
	}
	return result, nil
}

func importerSecretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "token") || strings.Contains(key, "code") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "verifier")
}

func redactImporterFields(fields map[string]any) map[string]any {
	result := make(map[string]any, len(fields))
	for key, value := range fields {
		if importerSecretKey(key) {
			result[key] = "[REDACTED]"
		} else {
			result[key] = value
		}
	}
	return result
}

func redactImporterResult(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	return redactImporterFields(object)
}
