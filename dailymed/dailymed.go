// Package dailymed is the library behind the dailymed command line:
// the HTTP client, request shaping, and the typed data models for DailyMed.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package dailymed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Host is the site this client talks to.
const Host = "dailymed.nlm.nih.gov"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// DefaultUserAgent identifies the client to DailyMed in a transparent, polite way.
const DefaultUserAgent = "dailymed-cli/0.1 (tamnd87@gmail.com)"

// DefaultRate is the minimum gap between requests.
const DefaultRate = 300 * time.Millisecond

// DefaultTimeout is the per-request HTTP timeout.
const DefaultTimeout = 15 * time.Second

// DefaultRetries is the number of retry attempts on transient errors.
const DefaultRetries = 3

// SPL represents a single Structured Product Label (drug label) record.
type SPL struct {
	SetID         string `kit:"id" json:"setid"`
	Title         string `json:"title"`
	PublishedDate string `json:"published_date"`
}

// NDC represents a National Drug Code record associated with an SPL.
type NDC struct {
	NDC string `kit:"id" json:"ndc"`
}

// wire types for JSON decoding

type splListResp struct {
	Data []SPL `json:"data"`
}

// packagingDataWire is the wrapper returned by the packaging.json endpoint.
// The `data` field is an object (not an array), so we decode it into this struct.
type packagingDataWire struct {
	SetID         string `json:"setid"`
	Title         string `json:"title"`
	PublishedDate string `json:"published_date"`
}

type packagingResp struct {
	Data packagingDataWire `json:"data"`
}

// ndcEntry is one element in the ndcs array returned by ndcs.json.
type ndcEntry struct {
	NDC string `json:"ndc"`
}

// ndcDataWire is the `data` object returned by ndcs.json.
type ndcDataWire struct {
	SetID         string     `json:"setid"`
	Title         string     `json:"title"`
	PublishedDate string     `json:"published_date"`
	NDCs          []ndcEntry `json:"ndcs"`
}

type ndcResp struct {
	Data ndcDataWire `json:"data"`
}

// Client talks to the DailyMed public API over HTTPS.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with the sensible defaults.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: DefaultTimeout},
		UserAgent: DefaultUserAgent,
		Rate:      DefaultRate,
		Retries:   DefaultRetries,
	}
}

// SearchSPLs searches DailyMed drug labels by drug name.
// limit is the page size, page is 1-based.
func (c *Client) SearchSPLs(ctx context.Context, query string, limit, page int) ([]SPL, error) {
	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	u := BaseURL + "/dailymed/services/v2/spls.json?drug_name=" +
		url.QueryEscape(query) +
		fmt.Sprintf("&pagesize=%d&page=%d", limit, page)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp splListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode spls: %w", err)
	}
	return resp.Data, nil
}

// GetSPL fetches a single SPL record by its setid (UUID string).
// It uses the packaging.json sub-endpoint which returns full SPL metadata.
func (c *Client) GetSPL(ctx context.Context, setid string) (*SPL, error) {
	u := BaseURL + "/dailymed/services/v2/spls/" + url.PathEscape(strings.TrimSpace(setid)) + "/packaging.json"
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp packagingResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode spl: %w", err)
	}
	d := resp.Data
	if d.SetID == "" {
		return nil, fmt.Errorf("spl %q not found", setid)
	}
	return &SPL{
		SetID:         d.SetID,
		Title:         d.Title,
		PublishedDate: d.PublishedDate,
	}, nil
}

// ListNDCs returns the NDC codes for the SPL identified by setid.
// limit caps the result slice; zero means no cap.
func (c *Client) ListNDCs(ctx context.Context, setid string, limit int) ([]NDC, error) {
	u := BaseURL + "/dailymed/services/v2/spls/" + url.PathEscape(strings.TrimSpace(setid)) + "/ndcs.json"
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp ndcResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode ndcs: %w", err)
	}
	out := make([]NDC, 0, len(resp.Data.NDCs))
	for _, e := range resp.Data.NDCs {
		out = append(out, NDC(e))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Get fetches a URL and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
