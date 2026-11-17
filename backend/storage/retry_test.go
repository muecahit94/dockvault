package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/minio/minio-go/v7"
)

func shortBackoff(t *testing.T) {
	t.Helper()
	prev := retryBackoff
	retryBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryBackoff = prev })
}

func TestWithRetryRecoversFromTransientErrors(t *testing.T) {
	shortBackoff(t)
	calls := 0
	err := withRetry(context.Background(), "test", isTransientNetworkError, func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("upload: %w", io.ErrUnexpectedEOF)
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("expected success after 3 calls, got err=%v calls=%d", err, calls)
	}
}

func TestWithRetryStopsOnPermanentErrorAndExhaustion(t *testing.T) {
	shortBackoff(t)

	calls := 0
	permanent := errors.New("access denied")
	if err := withRetry(context.Background(), "test", isTransientNetworkError, func() error {
		calls++
		return permanent
	}); !errors.Is(err, permanent) || calls != 1 {
		t.Fatalf("permanent error should not be retried: err=%v calls=%d", err, calls)
	}

	calls = 0
	if err := withRetry(context.Background(), "test", isTransientNetworkError, func() error {
		calls++
		return io.ErrUnexpectedEOF
	}); !errors.Is(err, io.ErrUnexpectedEOF) || calls != len(retryBackoff)+1 {
		t.Fatalf("expected %d attempts before giving up, got err=%v calls=%d", len(retryBackoff)+1, err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	_ = withRetry(ctx, "test", isTransientNetworkError, func() error {
		calls++
		return io.ErrUnexpectedEOF
	})
	if calls != 1 {
		t.Fatalf("cancelled context should stop retries, got %d calls", calls)
	}
}

func TestRetryClassifiers(t *testing.T) {
	cases := []struct {
		name string
		fn   func(error) bool
		err  error
		want bool
	}{
		{"s3 slowdown", isS3Retryable, fmt.Errorf("wrap: %w", minio.ErrorResponse{StatusCode: 503, Code: "SlowDown"}), true},
		{"s3 500", isS3Retryable, minio.ErrorResponse{StatusCode: 500, Code: "Whatever"}, true},
		{"s3 access denied", isS3Retryable, minio.ErrorResponse{StatusCode: 403, Code: "AccessDenied"}, false},
		{"s3 no such key", isS3Retryable, minio.ErrorResponse{StatusCode: 404, Code: "NoSuchKey"}, false},
		{"s3 network", isS3Retryable, io.ErrUnexpectedEOF, true},
		{"azure throttled", isAzureRetryable, &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}, true},
		{"azure 503", isAzureRetryable, fmt.Errorf("wrap: %w", &azcore.ResponseError{StatusCode: 503}), true},
		{"azure auth", isAzureRetryable, &azcore.ResponseError{StatusCode: http.StatusForbidden}, false},
		{"azure archived", isAzureRetryable, &azcore.ResponseError{StatusCode: http.StatusConflict}, false},
		{"context canceled", isAzureRetryable, context.Canceled, false},
	}
	for _, tc := range cases {
		if got := tc.fn(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
