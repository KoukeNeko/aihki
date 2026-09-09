package taiga

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type CSVDownload struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func csvResource(resource string) (string, string, error) {
	switch resource {
	case "epic":
		return "epics", "epics", nil
	case "story":
		return "userstories", "userstories", nil
	case "task":
		return "tasks", "tasks", nil
	case "issue":
		return "issues", "issues", nil
	default:
		return "", "", fmt.Errorf("CSV resource must be epic, story, task, or issue")
	}
}

func (c *Client) CreateCSVToken(ctx context.Context, projectID int64, resource string) (string, error) {
	_, field, err := csvResource(resource)
	if err != nil {
		return "", err
	}
	var result struct {
		UUID string `json:"uuid"`
	}
	_, err = c.Post(ctx, fmt.Sprintf("projects/%d/regenerate_%s_csv_uuid", projectID, field), nil, &result)
	return result.UUID, err
}

func (c *Client) RevokeCSVToken(ctx context.Context, projectID int64, resource string) error {
	_, field, err := csvResource(resource)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, fmt.Sprintf("projects/%d/delete_%s_csv_uuid", projectID, field), nil, nil)
	return err
}

func (c *Client) DownloadCSV(ctx context.Context, resource, uuid string, destination io.Writer) (CSVDownload, error) {
	path, _, err := csvResource(resource)
	if err != nil {
		return CSVDownload{}, err
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path + "/csv"})
	query := endpoint.Query()
	query.Set("uuid", uuid)
	endpoint.RawQuery = query.Encode()
	watch := c.watchTransfer(ctx)
	defer watch.stop()
	request, err := http.NewRequestWithContext(watch.ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return CSVDownload{}, err
	}
	// Taiga performs API content negotiation before its CSV action returns the
	// raw response; */* works across releases whose renderer list omits text/csv.
	request.Header.Set("Accept", "*/*")
	request.Header.Set("User-Agent", "taiga-cli/0.1")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		// A download the operator interrupted is not an upstream fault, and
		// marking it retryable invites an agent to start it again.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return CSVDownload{}, ctxErr
		}
		return CSVDownload{}, &Error{Kind: KindTransport, Operation: "GET " + endpoint.Path, Message: watch.explain("download CSV export"), Retryable: true, Cause: err}
	}
	c.log(http.MethodGet, endpoint.Path, response.StatusCode, time.Since(started))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		_ = response.Body.Close()
		return CSVDownload{}, decodeAPIError("GET "+endpoint.Path, response.StatusCode, data)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, hash), watch.reader(response.Body))
	closeErr := response.Body.Close()
	if copyErr != nil || closeErr != nil {
		if copyErr == nil {
			copyErr = closeErr
		}
		// Stopped by the operator mid-stream is the same interruption as
		// stopped before the first byte, not a fault to retry.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return CSVDownload{}, ctxErr
		}
		return CSVDownload{}, &Error{Kind: KindTransport, Operation: "GET " + endpoint.Path, Message: watch.explain("stream CSV export"), Retryable: true, Cause: copyErr}
	}
	return CSVDownload{Bytes: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
