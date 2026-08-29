package square

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietServer is an httptest server that does not log the deliberate
// connection drops these tests use.
func quietServer(handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	return srv
}

func TestRetriesServerErrorsThenSucceeds(t *testing.T) {
	var attempts int
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"value":"ok"}`)
	})
	defer srv.Close()

	var result struct {
		Value string `json:"value"`
	}
	require.NoError(t, NewClient(srv.URL, "tok").Get(context.Background(), "/v2/x", &result))

	assert.Equal(t, 3, attempts)
	assert.Equal(t, "ok", result.Value)
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var attempts int
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errors":[{"code":"BAD_REQUEST","detail":"nope"}]}`)
	})
	defer srv.Close()

	err := NewClient(srv.URL, "tok").Get(context.Background(), "/v2/x", nil)
	require.Error(t, err)

	// Retrying a request the server already rejected on its merits only
	// wastes the operator's time.
	assert.Equal(t, 1, attempts)
}

func TestRetriesDroppedConnections(t *testing.T) {
	var attempts int
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// Drops the connection without a response, the way a load
			// balancer recycling a keep-alive does.
			panic(http.ErrAbortHandler)
		}
		fmt.Fprint(w, `{}`)
	})
	defer srv.Close()

	require.NoError(t, NewClient(srv.URL, "tok").Get(context.Background(), "/v2/x", nil))
	assert.Equal(t, 2, attempts,
		"a transient connection drop mid-apply should not fail the whole run")
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var attempts int
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	})
	defer srv.Close()

	err := NewClient(srv.URL, "tok").Get(context.Background(), "/v2/x", nil)
	require.Error(t, err)
	assert.Equal(t, maxAttempts, attempts)
}

func TestCancellationIsReportedAsCancellation(t *testing.T) {
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := NewClient(srv.URL, "tok").Get(ctx, "/v2/x", nil)

	// An operator's Ctrl-C is not "request failed": the distinction is
	// what lets the command exit 130 instead of reporting a fault.
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "request failed")
}

func TestCancelledContextIsNotRetried(t *testing.T) {
	var attempts int
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewClient(srv.URL, "tok").Get(ctx, "/v2/x", nil)
	require.Error(t, err)
	assert.Equal(t, 0, attempts)
}

func TestRetryableNetworkErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancelled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"unknown host", &net.DNSError{Err: "no such host", IsNotFound: true}, false},
		{"dns server failure", &net.DNSError{Err: "server misbehaving"}, true},
		{"connection reset", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"plain error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, retryableNetworkError(tt.err))
		})
	}
}

func TestClassifyErrorParsesSquareShape(t *testing.T) {
	err := classifyError(http.StatusBadRequest,
		[]byte(`{"errors":[{"code":"INVALID_VALUE","detail":"percentage is not a number","field":"tax_data.percentage"}]}`),
		http.Header{})

	assert.Equal(t, "INVALID_VALUE", err.Code)
	assert.Equal(t, "percentage is not a number", err.Message)
	assert.Equal(t, "tax_data.percentage", err.Field)
	assert.False(t, err.Retryable)
	assert.Contains(t, err.Error(), "field: tax_data.percentage",
		"the field is what tells the operator which YAML key to fix")
}

func TestClassifyErrorTruncatesUnparseableBodies(t *testing.T) {
	// A proxy or WAF in front of Square answers with an HTML page.
	body := []byte("<html>" + strings.Repeat("x", 4000) + "</html>")

	err := classifyError(http.StatusBadGateway, body, http.Header{})

	assert.Less(t, len(err.Message), 700)
	assert.Contains(t, err.Message, "(truncated)")
}

func TestClassifyErrorFallsBackToStatusText(t *testing.T) {
	err := classifyError(http.StatusNotFound, nil, http.Header{})
	assert.Equal(t, "Not Found", err.Message)
}

func TestRetryClassification(t *testing.T) {
	tests := []struct {
		status    int
		retryable bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusConflict, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			err := classifyError(tt.status, nil, http.Header{})
			assert.Equal(t, tt.retryable, err.Retryable)
			assert.Equal(t, tt.retryable, isRetryable(err))
		})
	}
}

func TestAuthErrorsCarryAHint(t *testing.T) {
	unauthorized := classifyError(http.StatusUnauthorized,
		[]byte(`{"errors":[{"code":"UNAUTHORIZED","detail":"This request could not be authorized."}]}`),
		http.Header{})

	// "Square API error 401 (UNAUTHORIZED)" on its own leaves the
	// operator guessing whether the token, the environment, or the
	// account is at fault.
	assert.Contains(t, unauthorized.Error(), "mise init --force")

	forbidden := classifyError(http.StatusForbidden, nil, http.Header{})
	assert.Contains(t, forbidden.Error(), "ITEMS_WRITE")

	notFound := classifyError(http.StatusNotFound, nil, http.Header{})
	assert.Contains(t, notFound.Error(), "mise fetch")
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	assert.Equal(t, time.Duration(0), parseRetryAfter("garbage"))
	assert.Equal(t, time.Duration(0), parseRetryAfter("0"))
	assert.Equal(t, 5*time.Second, parseRetryAfter("5"))
	assert.Equal(t, 5*time.Second, parseRetryAfter("  5  "))

	// A date already in the past means "retry now", not "wait forever".
	assert.Equal(t, time.Duration(0),
		parseRetryAfter(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)))

	future := parseRetryAfter(time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat))
	assert.Greater(t, future, 25*time.Second)
}

func TestBackoffHonoursRetryAfter(t *testing.T) {
	err := &APIError{Retryable: true, RetryAfter: 3 * time.Second}
	assert.Equal(t, 3*time.Second, backoffFor(1, err))

	// Square knows when its quota resets; Mise's guess should not
	// override it.
	assert.Equal(t, 3*time.Second, backoffFor(3, err))
}

func TestBackoffCapsWhatSquareAsksFor(t *testing.T) {
	err := &APIError{Retryable: true, RetryAfter: time.Hour}
	assert.Equal(t, maxRetryDelay, backoffFor(1, err))
}

func TestBackoffIsJittered(t *testing.T) {
	err := errors.New("transient")

	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		delay := backoffFor(2, err)
		// Equal jitter: never below half the nominal interval, never above it.
		assert.GreaterOrEqual(t, delay, retryBaseDelay)
		assert.LessOrEqual(t, delay, 2*retryBaseDelay)
		seen[delay] = true
	}

	// Parallel location reads that all hit the same 429 must not retry
	// in lockstep and rebuild the burst that caused it.
	assert.Greater(t, len(seen), 1, "backoff should vary between callers")
}

func TestRateLimiterAllowsBurstThenThrottles(t *testing.T) {
	limiter := NewRateLimiter(50)

	// The bucket starts full, so the first burst is immediate.
	start := time.Now()
	for i := 0; i < 50; i++ {
		require.NoError(t, limiter.Wait(context.Background()))
	}
	assert.Less(t, time.Since(start), 100*time.Millisecond)

	// The next one has to wait for a token to refill.
	start = time.Now()
	require.NoError(t, limiter.Wait(context.Background()))
	assert.Greater(t, time.Since(start), 5*time.Millisecond)
}

func TestRateLimiterIsCancellable(t *testing.T) {
	limiter := NewRateLimiter(1)
	limiter.tokens = 0 // would otherwise wait a full second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := limiter.Wait(ctx)

	// A fetch across many locations spends most of its time queued here.
	// A Ctrl-C that only lands once the queue drains is not a Ctrl-C.
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestRateLimiterRejectsAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, NewRateLimiter(30).Wait(ctx), context.Canceled)
}

func TestRateLimiterIsConcurrencySafe(t *testing.T) {
	limiter := NewRateLimiter(200)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, limiter.Wait(context.Background()))
		}()
	}
	wg.Wait()
}

func TestRateLimiterRejectsNonPositiveRate(t *testing.T) {
	// A zero rate would divide by zero when computing the wait.
	limiter := NewRateLimiter(0)
	assert.Equal(t, float64(1), limiter.maxRate)
	require.NoError(t, limiter.Wait(context.Background()))
}

func TestClientSendsRequiredHeaders(t *testing.T) {
	var got http.Header
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		fmt.Fprint(w, `{}`)
	})
	defer srv.Close()

	require.NoError(t, NewClient(srv.URL, "secret-token").Get(context.Background(), "/v2/x", nil))

	assert.Equal(t, "Bearer secret-token", got.Get("Authorization"))
	assert.Equal(t, APIVersion, got.Get("Square-Version"))
	assert.Equal(t, "application/json", got.Get("Content-Type"))
	assert.True(t, strings.HasPrefix(got.Get("User-Agent"), "Mise/"))
}

func TestClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	var path string
	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprint(w, `{}`)
	})
	defer srv.Close()

	require.NoError(t, NewClient(srv.URL+"/", "tok").Get(context.Background(), "/v2/locations", nil))
	assert.Equal(t, "/v2/locations", path)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 10))
	assert.Equal(t, "abcde… (truncated)", truncate("abcdefghij", 5))
}
