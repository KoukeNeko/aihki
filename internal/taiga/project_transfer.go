package taiga

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"time"
)

type ProjectExportResult struct {
	ExportID string `json:"export_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

func (result ProjectExportResult) Accepted() bool { return result.ExportID != "" && result.URL == "" }

type ProjectImportResult struct {
	Project
	ImportID string `json:"import_id,omitempty"`
}

func (result ProjectImportResult) Accepted() bool {
	return result.ImportID != "" && result.ID == 0
}

func (c *Client) RequestProjectExport(ctx context.Context, projectID int64, dumpFormat string) (ProjectExportResult, error) {
	query := url.Values{"dump_format": []string{dumpFormat}}
	var result ProjectExportResult
	_, err := c.GetOnce(ctx, fmt.Sprintf("exporter/%d", projectID), query, &result)
	return result, err
}

func (c *Client) ImportProjectDump(ctx context.Context, name, contentType string, source io.Reader) (ProjectImportResult, error) {
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		partHeader := make(textproto.MIMEHeader)
		partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="dump"; filename=%q`, name))
		partHeader.Set("Content-Type", contentType)
		part, err := multipartWriter.CreatePart(partHeader)
		if err == nil {
			_, err = io.Copy(part, source)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			writeDone <- err
			return
		}
		writeDone <- pipeWriter.Close()
	}()

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "importer/load_dump"})
	watch := c.watchTransfer(ctx)
	defer watch.stop()
	request, err := http.NewRequestWithContext(watch.ctx, http.MethodPost, endpoint.String(), watch.reader(pipeReader))
	if err != nil {
		_ = pipeReader.Close()
		return ProjectImportResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("User-Agent", "taiga-cli/0.1")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	// Nothing has gone out yet, so a context that is already finished leaves
	// nothing to reconcile -- and an import creates an entire project.
	if err := ctx.Err(); err != nil {
		_ = pipeReader.CloseWithError(err)
		return ProjectImportResult{}, err
	}
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		message := watch.explain("project import may have been accepted; verify before retrying")
		if ctx.Err() != nil {
			message = "import was interrupted before Taiga confirmed it; verify before retrying"
		}
		return ProjectImportResult{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: message, Retryable: false, Cause: err}
	}
	c.log(http.MethodPost, endpoint.Path, response.StatusCode, time.Since(started))
	data, readErr := io.ReadAll(io.LimitReader(watch.reader(response.Body), maxResponseBytes))
	closeErr := response.Body.Close()
	writeErr := <-writeDone
	if readErr != nil || closeErr != nil {
		if readErr == nil {
			readErr = closeErr
		}
		return ProjectImportResult{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: watch.explain("project import may have been accepted; verify before retrying"), Retryable: false, Cause: readErr}
	}
	// The response outranks the write error for the same reason as an
	// attachment upload: Taiga rejects a dump without draining it, and that
	// rejection is the answer the caller needs.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProjectImportResult{}, decodeAPIError("POST "+endpoint.Path, response.StatusCode, data)
	}
	if writeErr != nil {
		return ProjectImportResult{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: "Taiga accepted the import before the dump finished sending, so it may be incomplete; verify before retrying", Retryable: false, Cause: writeErr}
	}
	var result ProjectImportResult
	if err := json.Unmarshal(data, &result); err != nil {
		return ProjectImportResult{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: "project import was accepted but its result could not be decoded", Retryable: false, Cause: err}
	}
	return result, nil
}
