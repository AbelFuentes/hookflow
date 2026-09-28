package action_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AbelFuentes/hookflow/internal/action"
	"github.com/AbelFuentes/hookflow/internal/engine"
)

func runHTTP(t *testing.T, params map[string]any, e engine.Event) error {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := action.DefaultRegistry(log)["http"](params)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return a.Run(context.Background(), e)
}

type captured struct {
	method, header, contentType string
	body                        map[string]any
}

func TestHTTP_SendsEventAsJSON(t *testing.T) {
	ch := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := captured{method: r.Method, header: r.Header.Get("X-Source"), contentType: r.Header.Get("Content-Type")}
		_ = json.NewDecoder(r.Body).Decode(&c.body)
		ch <- c
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := runHTTP(t,
		map[string]any{"url": srv.URL, "headers": map[string]any{"X-Source": "hookflow"}},
		engine.Event{Source: "alexa", Name: "GoodNight", Payload: map[string]any{"room": "bedroom"}},
	)
	if err != nil {
		t.Fatal(err)
	}

	got := <-ch
	if got.method != http.MethodPost || got.header != "hookflow" || got.contentType != "application/json" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if got.body["source"] != "alexa" || got.body["name"] != "GoodNight" {
		t.Fatalf("unexpected body: %+v", got.body)
	}
}

func TestHTTP_RetriesOnServerErrorThenSucceeds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := runHTTP(t, map[string]any{"url": srv.URL, "retries": 3, "backoff": "1ms"}, engine.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}
}

func TestHTTP_DoesNotRetryOnClientError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	err := runHTTP(t, map[string]any{"url": srv.URL, "backoff": "1ms"}, engine.Event{})
	if err == nil {
		t.Fatal("expected error")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestHTTP_GivesUpAfterMaxRetries(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := runHTTP(t, map[string]any{"url": srv.URL, "retries": 2, "backoff": "1ms"}, engine.Event{})
	if err == nil || !strings.Contains(err.Error(), "giving up after 3 attempts") {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}
}

func TestHTTP_TimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	err := runHTTP(t, map[string]any{"url": srv.URL, "timeout": "20ms", "retries": 0}, engine.Event{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHTTP_InvalidParams(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name    string
		params  map[string]any
		wantErr string
	}{
		{"missing url", map[string]any{}, "valid http(s) URL"},
		{"bad scheme", map[string]any{"url": "ftp://x.com"}, "valid http(s) URL"},
		{"bad method", map[string]any{"url": "http://x.com", "method": "TRACE"}, "unsupported method"},
		{"unknown param", map[string]any{"url": "http://x.com", "foo": 1}, "unknown param"},
		{"negative retries", map[string]any{"url": "http://x.com", "retries": -1}, "retries"},
		{"bad timeout", map[string]any{"url": "http://x.com", "timeout": "abc"}, "timeout"},
		{"bad headers", map[string]any{"url": "http://x.com", "headers": "nope"}, "headers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := action.DefaultRegistry(log)["http"](tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
