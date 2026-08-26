package square

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

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
			Timeout: 30 * time.Second,
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

// do is the internal method that executes all HTTP requests.
// It handles auth headers, rate limiting, and error classification.
func (c *Client) do(ctx context.Context, method string, path string, body interface{}, result interface{}) error {
	// Wait for rate limiter
	c.rateLimiter.Wait()

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
	req.Header.Set("Square-Version", "2024-01-18") // Pin API version for stability
	req.Header.Set("User-Agent", "Mise/0.1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Classify errors
	if resp.StatusCode >= 400 {
		return classifyError(resp.StatusCode, respBody)
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
	Retryable  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Square API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// classifyError parses a Square error response and determines
// whether it's retryable.
func classifyError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Code:       http.StatusText(statusCode),
		Message:    string(body),
	}

	// Try to parse structured error
	var parsed struct {
		Errors []struct {
			Code    string `json:"code"`
			Detail  string `json:"detail"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &parsed) == nil && len(parsed.Errors) > 0 {
		apiErr.Code = parsed.Errors[0].Code
		apiErr.Message = parsed.Errors[0].Detail
	}

	// 429 (rate limit) and 5xx (server errors) are retryable
	apiErr.Retryable = statusCode == 429 || statusCode >= 500

	return apiErr
}

// RateLimiter implements a simple token-bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	tokens   int
	maxRate  int
	lastTick time.Time
}

// NewRateLimiter creates a rate limiter that allows maxRate
// requests per second.
func NewRateLimiter(maxRate int) *RateLimiter {
	return &RateLimiter{
		tokens:   maxRate,
		maxRate:  maxRate,
		lastTick: time.Now(),
	}
}

// Wait blocks until a request token is available.
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTick)

	// Replenish tokens based on elapsed time
	newTokens := int(elapsed.Seconds() * float64(rl.maxRate))
	if newTokens > 0 {
		rl.tokens += newTokens
		if rl.tokens > rl.maxRate {
			rl.tokens = rl.maxRate
		}
		rl.lastTick = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return
	}

	// No tokens available — wait for one to replenish
	sleepDuration := time.Second / time.Duration(rl.maxRate)
	rl.mu.Unlock()
	time.Sleep(sleepDuration)
	rl.mu.Lock()
	rl.lastTick = time.Now()
}
