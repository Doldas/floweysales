package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedGETRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	body, err := New("test", 0).Get(context.Background(), srv.URL, nil)
	if err != nil || string(body) != `{"ok":true}` || calls.Load() != 3 {
		t.Fatalf("body=%s err=%v calls=%d", body, err, calls.Load())
	}
}

func TestDoesNotRetryForbidden(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := New("test", 0).Get(context.Background(), srv.URL, nil); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRetryAfterIsHonoredAndBounded(t *testing.T) {
	var calls atomic.Int32
	var first, second time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			first = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			second = time.Now()
			_, _ = w.Write([]byte(`ok`))
		}
	}))
	defer srv.Close()
	if _, err := New("test", 0).Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if gap := second.Sub(first); gap < 950*time.Millisecond {
		t.Fatalf("retried after %s, before the Retry-After of 1s", gap)
	}
}

func TestRetryAfterBeyondLimitFailsFastWithHint(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := New("test", 0).Get(context.Background(), srv.URL, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || se.RetryAfter != 120*time.Second || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{
		"": 0, "7": 7 * time.Second, "-3": 0, "soon": 0,
		now.Add(30 * time.Second).Format(http.TimeFormat): 30 * time.Second,
		now.Add(-time.Minute).Format(http.TimeFormat):     0,
	} {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
	}
}

func TestContextCancelStopsRetryWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "4")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := New("test", 0).Get(ctx, srv.URL, nil); err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestSlowProviderTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()
	c := New("test", 0)
	c.HTTP.Timeout = 100 * time.Millisecond
	if _, err := c.Get(context.Background(), srv.URL, nil); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestToFloatIsStrict(t *testing.T) {
	for in, want := range map[any]float64{"12.5": 12.5, " 3 ": 3, "12abc": 0, "NaN": 0, "Inf": 0, "-Inf": 0, "": 0, 4.0: 4, nil: 0} {
		if got := ToFloat(in); got != want {
			t.Errorf("%v: got %v want %v", in, got, want)
		}
	}
}
