package square

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vaarde/mise/internal/version"
)

const (
	// APIVersion pins the Square API version so that Square's own
	// rollouts cannot change response shapes under Mise.
	APIVersion = "2024-01-18"

	// requestTimeout bounds a single HTTP request. A batch-upsert of
	// several thousand catalog objects is a genuinely slow call, so this
	// is generous; a hung connection is caught by it rather than by the
	// operator giving up.
	requestTimeout = 60 * time.Second

	// maxAttempts bounds retries of retryable failures (429, 5xx, and
	// transient network errors).
	maxAttempts = 3

	// retryBaseDelay is the first backoff interval; it doubles per attempt.
	retryBaseDelay = 500 * time.Millisecond

	// maxRetryDelay caps a single backoff, including one Square asked
	// for via Retry-After. Waiting longer than this without output looks
	// like a hang.
	maxRetryDelay = 30 * time.Second

	// maxErrorBodyBytes bounds how much of an unparseable error response
	// is quoted back to the operator. A proxy or WAF in front of Square
	// answers with an HTML page, and dumping that into a terminal buries
	// everything else the command said.
	maxErrorBodyBytes = 512
)

// UserAgent identifies Mise in Square's API logs.
var UserAgent = "Mise/" + version.Current().Version

// Client is an HTTP client for the Square API. It handles
// authentication headers, rate limiting, and JSON serialization.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
	rateLimiter *RateLimiter
}

// NewClient creates a Square API client.
func NewClient(baseURL string, accessToken string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		baseURL:     strings.TrimRight(baseURL, "/"),
		accessToken: accessToken,
		rateLimiter: NewRateLimiter(30), // ~30 req/s to stay under Square's ~40/s limit
	}
}

// Get performs an authenticated GET request and decodes the JSON response.
func (c *Client) Get(ctx context.Context, path string, result interface{}) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

// Post performs an authenticated POST request with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body interface{}, result interface{}) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

// Put performs an authenticated PUT request with a JSON body.
func (c *Client) Put(ctx context.Context, path string, body interface{}, result interface{}) error {
	return c.do(ctx, http.MethodPut, path, body, result)
}

// Delete performs an authenticated DELETE request.
func (c *Client) Delete(ctx context.Context, path string, result interface{}) error {
	return c.do(ctx, http.MethodDelete, path, nil, result)
}

// do executes a request, retrying retryable failures (429 rate limit,
// 5xx server errors, and transient network faults) with exponential
// backoff. Non-retryable errors (400, 401, 404) are returned
// immediately — retrying a bad request only wastes the operator's time.
func (c *Client) do(ctx context.Context, method string, path string, body interface{}, result interface{}) error {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = c.attempt(ctx, method, path, body, result)
		if lastErr == nil {
			return nil
		}

		if attempt == maxAttempts || !isRetryable(lastErr) {
			return lastErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffFor(attempt, lastErr)):
		}
	}

	return lastErr
}

// backoffFor returns how long to wait before the next attempt.
//
// Square's Retry-After wins when it sent one: it knows when the quota
// resets and Mise does not. Otherwise the delay doubles per attempt,
// with jitter — a fetch reads locations in parallel, so without jitter
// every goroutine that hit the same 429 would retry in lockstep and
// rebuild the burst that caused it.
func backoffFor(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		if apiErr.RetryAfter > maxRetryDelay {
			return maxRetryDelay
		}
		return apiErr.RetryAfter
	}

	delay := retryBaseDelay * time.Duration(1<<(attempt-1))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	// Equal jitter: half the interval fixed, half random.
	return delay/2 + time.Duration(rand.Int63n(int64(delay/2)+1))
}

// isRetryable reports whether an attempt is worth repeating.
func isRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}
	return retryableNetworkError(err)
}

// retryableNetworkError reports whether a transport-level failure is
// transient.
//
// A connection reset or a dropped keep-alive mid-apply used to fail the
// whole run, even though the next attempt would have succeeded. What is
// deliberately not retried: a cancelled context, because the operator
// asked to stop, and an unresolvable host, because a typo will not fix
// itself on the second try.
func retryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// attempt performs a single HTTP request. It handles auth headers,
// rate limiting, and error classification.
func (c *Client) attempt(ctx context.Context, method string, path string, body interface{}, result interface{}) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Square requires Bearer token auth and JSON content type
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", APIVersion)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// A cancelled run is not a request failure; report it as the
		// cancellation it is so callers can tell the two apart.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Classify errors
	if resp.StatusCode >= 400 {
		return classifyError(resp.StatusCode, respBody, resp.Header)
	}

	// Decode successful response
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// APIError represents a structured error from the Square API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Field      string
	Retryable  bool

	// RetryAfter is how long Square asked Mise to wait, when it said.
	RetryAfter time.Duration

	// Hint is advice for the operator: what this status usually means
	// for a Mise workspace, and what to do about it.
	Hint string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("Square API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
	if e.Field != "" {
		msg += fmt.Sprintf(" [field: %s]", e.Field)
	}
	if e.Hint != "" {
		msg += "\n" + e.Hint
	}
	return msg
}

// classifyError parses a Square error response and determines
// whether it's retryable.
func classifyError(statusCode int, body []byte, header http.Header) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Code:       http.StatusText(statusCode),
		Message:    truncate(strings.TrimSpace(string(body)), maxErrorBodyBytes),
	}

	// Try to parse structured error
	var parsed struct {
		Errors []struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
			Field  string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &parsed) == nil && len(parsed.Errors) > 0 {
		apiErr.Code = parsed.Errors[0].Code
		apiErr.Message = parsed.Errors[0].Detail
		apiErr.Field = parsed.Errors[0].Field
	}

	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(statusCode)
	}

	// 429 (rate limit) and 5xx (server errors) are retryable
	apiErr.Retryable = statusCode == 429 || statusCode >= 500
	apiErr.RetryAfter = parseRetryAfter(header.Get("Retry-After"))
	apiErr.Hint = hintFor(statusCode)

	return apiErr
}

// hintFor turns a bare status code into something an operator can act
// on. A 401 that says only "UNAUTHORIZED" leaves them guessing whether
// the problem is the token, the environment, or the account.
func hintFor(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "The access token was rejected. It may have expired, been revoked, or belong to a\n" +
			"different environment than mise.yaml names. Run 'mise init --force' to re-authorize."
	case http.StatusForbidden:
		return "The token is valid but lacks the permissions this call needs. Square tokens are\n" +
			"scoped: reading the catalog needs ITEMS_READ and MERCHANT_PROFILE_READ, and writing\n" +
			"needs ITEMS_WRITE. Re-authorize with those scopes granted."
	case http.StatusNotFound:
		return "The resource no longer exists on the POS. Run 'mise fetch' to re-sync state."
	case http.StatusConflict:
		return "Another change landed first. Run 'mise plan' to rebuild the diff against current state."
	case http.StatusTooManyRequests:
		return "Square is rate limiting this account. Lower --parallelism and try again."
	}
	if statusCode >= 500 {
		return "Square reported a server-side error. This is usually transient — try again shortly."
	}
	return ""
}

// parseRetryAfter reads a Retry-After header in either of its forms:
// a delay in seconds, or an HTTP date.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

// truncate shortens s to at most n bytes, marking that it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "… (truncated)"
}

// RateLimiter implements a token-bucket rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	tokens  float64
	maxRate float64
	last    time.Time
}

// NewRateLimiter creates a rate limiter that allows maxRate
// requests per second.
func NewRateLimiter(maxRate int) *RateLimiter {
	if maxRate <= 0 {
		maxRate = 1
	}
	return &RateLimiter{
		tokens:  float64(maxRate),
		maxRate: float64(maxRate),
		last:    time.Now(),
	}
}

// Wait blocks until a request token is available, or until the context
// is cancelled.
//
// Cancellation matters here: a fetch across many locations spends most
// of its time queued behind this limiter, and a Ctrl-C that only takes
// effect once the queue drains is not a Ctrl-C.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		rl.mu.Lock()
		now := time.Now()
		rl.tokens += now.Sub(rl.last).Seconds() * rl.maxRate
		if rl.tokens > rl.maxRate {
			rl.tokens = rl.maxRate
		}
		rl.last = now

		if rl.tokens >= 1 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}

		// Sleep for exactly the shortfall rather than a fixed slice, so
		// the bucket refills to one token and no further.
		wait := time.Duration((1 - rl.tokens) / rl.maxRate * float64(time.Second))
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
