package cli

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

type projectExportView struct {
	Project   string `json:"project"`
	ProjectID int64  `json:"project_id"`
	Format    string `json:"format"`
	Status    string `json:"status"`
	ExportID  string `json:"export_id,omitempty"`
	URL       string `json:"url,omitempty"`
	Verified  bool   `json:"verified"`
}

type projectImportView struct {
	Source   string         `json:"source"`
	Format   string         `json:"format"`
	Status   string         `json:"status"`
	ImportID string         `json:"import_id,omitempty"`
	Project  *taiga.Project `json:"project,omitempty"`
	Verified bool           `json:"verified"`
}

type projectDumpMetadata struct {
	Name      string
	Slug      string
	IsPrivate *bool
}

type projectDumpSource struct {
	File        *os.File
	DisplayName string
	UploadName  string
	Format      string
	ContentType string
	Size        int64
	Metadata    projectDumpMetadata
	cleanup     func()
}

func (a *App) projectExportCommand() *cobra.Command {
	var dumpFormat string
	var dryRun bool
	command := &cobra.Command{
		Use: "export <slug>", Short: "Request a portable Taiga project dump", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dumpFormat != "plain" && dumpFormat != "gzip" {
				return usageError("--format must be plain or gzip")
			}
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			project, err := client.GetProjectBySlug(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if dryRun {
				return a.renderDryRun("request project export", project.Slug, map[string]any{"project_id": project.ID, "format": dumpFormat, "may_be_asynchronous": true})
			}
			result, err := client.RequestProjectExport(cmd.Context(), project.ID, dumpFormat)
			if err != nil {
				return err
			}
			view := projectExportView{Project: project.Slug, ProjectID: project.ID, Format: dumpFormat}
			switch {
			case result.URL != "":
				view.Status, view.URL, view.Verified = "ready", result.URL, true
			case result.ExportID != "":
				view.Status, view.ExportID = "accepted", result.ExportID
			default:
				return validationError("invalid_export_response", "Taiga accepted the export request without a URL or export ID")
			}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			if a.global.Quiet {
				return nil
			}
			if view.Status == "ready" {
				_, _ = fmt.Fprintf(a.Out, "Project export ready: %s\n", view.URL)
			} else {
				_, _ = fmt.Fprintf(a.Out, "Project export accepted (%s); Taiga will email the download link when ready\n", view.ExportID)
			}
			return nil
		},
	}
	command.Flags().StringVar(&dumpFormat, "format", "gzip", "dump format: plain or gzip")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "resolve and display the export request without enqueuing work")
	command.ValidArgsFunction = a.completeProjects
	return command
}

func (a *App) projectImportCommand() *cobra.Command {
	var yes, dryRun bool
	command := &cobra.Command{
		Use: "import <dump.json|dump.json.gz|->", Short: "Create a project from a Taiga dump", Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "-" && !dryRun && !yes {
				return confirmationRequired("project import from stdin requires --yes so confirmation does not consume dump data")
			}
			source, err := prepareProjectDump(a.In, args[0])
			if err != nil {
				return err
			}
			defer source.close()
			private := any(nil)
			if source.Metadata.IsPrivate != nil {
				private = *source.Metadata.IsPrivate
			}
			if dryRun {
				return a.renderDryRun("import project", source.DisplayName, map[string]any{
					"format": source.Format, "bytes": source.Size, "name": source.Metadata.Name,
					"slug": source.Metadata.Slug, "private": private,
				})
			}
			if !yes {
				if a.global.NoInput || !a.stdinTTY() {
					return confirmationRequired("project import creates a new project and requires --yes in non-interactive mode")
				}
				answer, err := a.readLine("Type IMPORT to create a project from this dump: ")
				if err != nil {
					return err
				}
				if answer != "IMPORT" {
					return confirmationRequired("project import was not confirmed")
				}
			}
			client, _, err := a.client(cmd.Context(), true)
			if err != nil {
				return err
			}
			// The dump is streamed, and a stream is not resent after a
			// refresh the way a JSON request is. Every other command reaches
			// Taiga through a JSON lookup before it streams, which is where
			// an expired token gets refreshed; import has no lookup of its
			// own, so the credential is exercised here first.
			if _, err := client.Me(cmd.Context()); err != nil {
				return err
			}
			if _, err := source.File.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("rewind project dump: %w", err)
			}
			result, err := client.ImportProjectDump(cmd.Context(), source.UploadName, source.ContentType, source.File)
			if err != nil {
				return err
			}
			view := projectImportView{Source: source.DisplayName, Format: source.Format}
			switch {
			case result.ID != 0:
				view.Status, view.Project, view.Verified = "created", &result.Project, true
			case result.ImportID != "":
				view.Status, view.ImportID = "accepted", result.ImportID
			default:
				return validationError("invalid_import_response", "Taiga accepted the import without a project or import ID")
			}
			if a.global.JSON {
				return a.renderer().Data(view)
			}
			if a.global.Quiet {
				return nil
			}
			if view.Status == "created" {
				_, _ = fmt.Fprintf(a.Out, "Imported project %s (%s)\n", view.Project.Name, view.Project.Slug)
			} else {
				_, _ = fmt.Fprintf(a.Out, "Project import accepted (%s); Taiga will email the result when complete\n", view.ImportID)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "confirm creation of a project from the dump")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate the dump and show the import plan without uploading")
	return command
}

func prepareProjectDump(input io.Reader, path string) (*projectDumpSource, error) {
	var file *os.File
	var cleanup func()
	displayName := path
	if path == "-" {
		temporary, err := os.CreateTemp("", "taiga-project-import-*.dump")
		if err != nil {
			return nil, fmt.Errorf("create temporary project dump: %w", err)
		}
		cleanup = func() {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
		if err := temporary.Chmod(0o600); err != nil {
			cleanup()
			return nil, fmt.Errorf("secure temporary project dump: %w", err)
		}
		if _, err := io.Copy(temporary, input); err != nil {
			cleanup()
			return nil, fmt.Errorf("read project dump from stdin: %w", err)
		}
		file, displayName = temporary, "stdin"
	} else {
		opened, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open project dump: %w", err)
		}
		file = opened
		cleanup = func() { _ = opened.Close() }
	}
	stat, err := file.Stat()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("inspect project dump: %w", err)
	}
	if stat.Size() == 0 {
		cleanup()
		return nil, validationError("invalid_project_dump", "project dump is empty")
	}
	compressed, err := isGzipFile(file)
	if err != nil {
		cleanup()
		return nil, err
	}
	metadata, err := inspectProjectDump(file, compressed)
	if err != nil {
		cleanup()
		return nil, validationError("invalid_project_dump", err.Error())
	}
	format, contentType, uploadName := "plain", "application/json", filepath.Base(path)
	if path == "-" {
		uploadName = "project.json"
	}
	if compressed {
		format, contentType = "gzip", "application/gzip"
		if path == "-" {
			uploadName = "project.json.gz"
		}
	}
	return &projectDumpSource{File: file, DisplayName: displayName, UploadName: uploadName, Format: format, ContentType: contentType, Size: stat.Size(), Metadata: metadata, cleanup: cleanup}, nil
}

func (source *projectDumpSource) close() {
	if source != nil && source.cleanup != nil {
		source.cleanup()
	}
}

func isGzipFile(file *os.File) (bool, error) {
	var header [2]byte
	n, err := file.Read(header[:])
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read project dump header: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("rewind project dump: %w", err)
	}
	return n == 2 && header[0] == 0x1f && header[1] == 0x8b, nil
}

func inspectProjectDump(file *os.File, compressed bool) (projectDumpMetadata, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return projectDumpMetadata{}, fmt.Errorf("rewind project dump: %w", err)
	}
	var reader io.Reader = file
	var gzipReader *gzip.Reader
	if compressed {
		var err error
		gzipReader, err = gzip.NewReader(file)
		if err != nil {
			return projectDumpMetadata{}, fmt.Errorf("open gzip project dump: %w", err)
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}
	decoder := json.NewDecoder(reader)
	first, err := decoder.Token()
	if err != nil {
		return projectDumpMetadata{}, fmt.Errorf("decode project dump: %w", err)
	}
	if delimiter, ok := first.(json.Delim); !ok || delimiter != '{' {
		return projectDumpMetadata{}, fmt.Errorf("project dump must be a JSON object")
	}
	metadata := projectDumpMetadata{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return projectDumpMetadata{}, fmt.Errorf("decode project dump key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return projectDumpMetadata{}, fmt.Errorf("project dump contains a non-string key")
		}
		switch key {
		case "name":
			if err := decoder.Decode(&metadata.Name); err != nil {
				return projectDumpMetadata{}, fmt.Errorf("decode project name: %w", err)
			}
		case "slug":
			if err := decoder.Decode(&metadata.Slug); err != nil {
				return projectDumpMetadata{}, fmt.Errorf("decode project slug: %w", err)
			}
		case "is_private":
			var value bool
			if err := decoder.Decode(&value); err != nil {
				return projectDumpMetadata{}, fmt.Errorf("decode project privacy: %w", err)
			}
			metadata.IsPrivate = &value
		default:
			if err := skipJSONValue(decoder); err != nil {
				return projectDumpMetadata{}, fmt.Errorf("decode project dump field %q: %w", key, err)
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return projectDumpMetadata{}, fmt.Errorf("close project dump object: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return projectDumpMetadata{}, fmt.Errorf("project dump contains trailing JSON values")
		}
		return projectDumpMetadata{}, fmt.Errorf("decode trailing project dump data: %w", err)
	}
	if strings.TrimSpace(metadata.Name) == "" {
		return projectDumpMetadata{}, fmt.Errorf("project dump does not contain a project name")
	}
	return metadata, nil
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	_, err = decoder.Token()
	return err
}
