package storage

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"
)

// retryBackoff defines the wait before each retry attempt (len+1 total attempts).
// It is a package variable so tests can shorten it.
var retryBackoff = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}

// withRetry runs fn and retries it with backoff while isRetryable reports a transient failure.
// SDK clients already retry individual HTTP requests; this adds operation-level retries
// (e.g. a whole multipart upload) for longer outages such as dropped connections.
func withRetry(ctx context.Context, op string, isRetryable func(error) bool, fn func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt >= len(retryBackoff) || ctx.Err() != nil || !isRetryable(err) {
			return err
		}
		wait := retryBackoff[attempt]
		log.Printf("dockvault: %s failed (attempt %d/%d), retrying in %s: %v", op, attempt+1, len(retryBackoff)+1, wait, err)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

// isTransientNetworkError reports connection-level failures that are worth retrying.
func isTransientNetworkError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// isRetryableHTTPStatus reports server-side throttling or transient availability errors.
func isRetryableHTTPStatus(code int) bool {
	return code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests ||
		code >= http.StatusInternalServerError
}
