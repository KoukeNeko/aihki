package taiga

import ( // nosemgrep -- for crypto/sha1, which says why it is here
	"context"
	"crypto/sha1" // #nosec G505 -- Taiga exposes SHA-1 for compatibility verification; SHA-256 is also computed.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AttachmentDownload struct {
	Bytes  int64  `json:"bytes"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256"`
}

func attachmentPath(resource string) (string, error) {
	switch resource {
	case "issue":
		return "issues/attachments", nil
	case "story":
		return "userstories/attachments", nil
	case "task":
		return "tasks/attachments", nil
	case "epic":
		return "epics/attachments", nil
	case "wiki":
		return "wiki/attachments", nil
	default:
		return "", fmt.Errorf("unsupported attachment resource %q", resource)
	}
}

func (c *Client) ListAttachments(ctx context.Context, resource string, projectID, objectID int64) ([]Attachment, error) {
	path, err := attachmentPath(resource)
	if err != nil {
		return nil, err
	}
	query := url.Values{"project": []string{strconv.FormatInt(projectID, 10)}, "object_id": []string{strconv.FormatInt(objectID, 10)}, "page_size": []string{"1000"}}
	var attachments []Attachment
	_, err = c.Get(ctx, path, query, &attachments)
	return attachments, err
}

func (c *Client) GetAttachment(ctx context.Context, resource string, id int64) (Attachment, error) {
	path, err := attachmentPath(resource)
	if err != nil {
		return Attachment{}, err
	}
	var attachment Attachment
	_, err = c.Get(ctx, fmt.Sprintf("%s/%d", path, id), nil, &attachment)
	return attachment, err
}

func (c *Client) CreateAttachment(ctx context.Context, resource string, projectID, objectID int64, name, description string, deprecated bool, source io.Reader) (Attachment, error) {
	path, err := attachmentPath(resource)
	if err != nil {
		return Attachment{}, err
	}
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeDone := make(chan error, 1)
	go func() {
		defer close(writeDone)
		fields := map[string]string{
			"project":       strconv.FormatInt(projectID, 10),
			"object_id":     strconv.FormatInt(objectID, 10),
			"description":   description,
			"is_deprecated": strconv.FormatBool(deprecated),
			"from_comment":  "false",
		}
		for key, value := range fields {
			if err := multipartWriter.WriteField(key, value); err != nil {
				_ = pipeWriter.CloseWithError(err)
				writeDone <- err
				return
			}
		}
		part, err := multipartWriter.CreateFormFile("attached_file", name)
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

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path})
	watch := c.watchTransfer(ctx)
	defer watch.stop()
	request, err := http.NewRequestWithContext(watch.ctx, http.MethodPost, endpoint.String(), watch.reader(pipeReader))
	if err != nil {
		_ = pipeReader.Close()
		return Attachment{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("User-Agent", "taiga-cli/0.1")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	// Nothing has gone out yet, so a context that is already finished leaves
	// nothing to reconcile.
	if err := ctx.Err(); err != nil {
		_ = pipeReader.CloseWithError(err)
		return Attachment{}, err
	}
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		_ = pipeReader.CloseWithError(err)
		message := watch.explain("attachment may have been uploaded; verify before retrying")
		if ctx.Err() != nil {
			message = "upload was interrupted before Taiga confirmed it; verify before retrying"
		}
		return Attachment{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: message, Retryable: false, Cause: err}
	}
	c.log(http.MethodPost, endpoint.Path, response.StatusCode, time.Since(started))
	data, readErr := io.ReadAll(io.LimitReader(watch.reader(response.Body), maxResponseBytes))
	closeErr := response.Body.Close()
	writeErr := <-writeDone
	if readErr != nil || closeErr != nil {
		if readErr == nil {
			readErr = closeErr
		}
		return Attachment{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: watch.explain("attachment may have been uploaded; verify before retrying"), Retryable: false, Cause: readErr}
	}
	// The response is read before the write error, because Taiga answers a
	// rejected or oversized upload without draining the body, which fails the
	// pipe writer. Reporting that failure first would throw away a completed
	// answer -- including a 201 -- and tell the caller an upload failed when
	// the attachment exists.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Attachment{}, decodeAPIError("POST "+endpoint.Path, response.StatusCode, data)
	}
	if writeErr != nil {
		return Attachment{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: "Taiga accepted the upload before the file finished sending, so it may be incomplete; verify before retrying", Retryable: false, Cause: writeErr}
	}
	var attachment Attachment
	if err := json.Unmarshal(data, &attachment); err != nil {
		return Attachment{}, &Error{Kind: KindAmbiguousCommit, Operation: "POST " + endpoint.Path, Message: "attachment was accepted but its result could not be decoded", Retryable: false, Cause: err}
	}
	return attachment, nil
}

func (c *Client) UpdateAttachment(ctx context.Context, resource string, id int64, request UpdateAttachmentRequest) (Attachment, error) {
	path, err := attachmentPath(resource)
	if err != nil {
		return Attachment{}, err
	}
	var attachment Attachment
	_, err = c.Patch(ctx, fmt.Sprintf("%s/%d", path, id), request, &attachment)
	return attachment, err
}

func (c *Client) DeleteAttachment(ctx context.Context, resource string, id int64) error {
	path, err := attachmentPath(resource)
	if err != nil {
		return err
	}
	return c.Delete(ctx, fmt.Sprintf("%s/%d", path, id))
}

func (c *Client) DownloadAttachment(ctx context.Context, attachment Attachment, destination io.Writer) (AttachmentDownload, error) {
	parsed, err := url.Parse(attachment.URL)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("parse attachment URL: %w", err)
	}
	if !parsed.IsAbs() {
		parsed = c.baseURL.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return AttachmentDownload{}, fmt.Errorf("unsupported attachment URL scheme %q", parsed.Scheme)
	}
	parsed.Fragment = ""
	watch := c.watchTransfer(ctx)
	defer watch.stop()
	request, err := http.NewRequestWithContext(watch.ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return AttachmentDownload{}, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "taiga-cli/0.1")
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		// A download the operator interrupted is not an upstream fault, and
		// marking it retryable invites an agent to start it again.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AttachmentDownload{}, ctxErr
		}
		return AttachmentDownload{}, &Error{Kind: KindTransport, Operation: "GET " + parsed.Path, Message: watch.explain("download attachment"), Retryable: true, Cause: err}
	}
	c.log(http.MethodGet, parsed.Path, response.StatusCode, time.Since(started))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		_ = response.Body.Close()
		return AttachmentDownload{}, decodeAPIError("GET "+parsed.Path, response.StatusCode, data)
	}
	sha1Hash := sha1.New() // #nosec G401 -- compatibility check against Taiga's stored digest. nosemgrep
	sha256Hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, sha1Hash, sha256Hash), watch.reader(response.Body))
	closeErr := response.Body.Close()
	if copyErr != nil || closeErr != nil {
		if copyErr == nil {
			copyErr = closeErr
		}
		// Stopped by the operator mid-stream is the same interruption as
		// stopped before the first byte, not a fault to retry.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AttachmentDownload{}, ctxErr
		}
		return AttachmentDownload{}, &Error{Kind: KindTransport, Operation: "GET " + parsed.Path, Message: watch.explain("stream attachment download"), Retryable: true, Cause: copyErr}
	}
	result := AttachmentDownload{Bytes: written, SHA1: hex.EncodeToString(sha1Hash.Sum(nil)), SHA256: hex.EncodeToString(sha256Hash.Sum(nil))}
	if attachment.Size > 0 && written != attachment.Size {
		return result, &Error{Kind: KindTransport, Operation: "GET " + parsed.Path, Message: fmt.Sprintf("attachment size mismatch: expected %d bytes, received %d", attachment.Size, written), Retryable: true}
	}
	if attachment.SHA1 != "" && !strings.EqualFold(attachment.SHA1, result.SHA1) {
		return result, &Error{Kind: KindTransport, Operation: "GET " + parsed.Path, Message: "attachment SHA-1 mismatch", Retryable: true}
	}
	return result, nil
}

func NormalizeAttachmentResource(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "issue", "issues":
		return "issue", nil
	case "story", "stories", "userstory", "userstories", "us":
		return "story", nil
	case "task", "tasks":
		return "task", nil
	case "epic", "epics":
		return "epic", nil
	case "wiki", "wikis":
		return "wiki", nil
	default:
		return "", fmt.Errorf("resource must be epic, story, task, issue, or wiki")
	}
}
