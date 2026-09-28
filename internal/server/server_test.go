package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbelFuentes/hookflow/internal/engine"
	"github.com/AbelFuentes/hookflow/internal/server"
)

type fakeHandler struct {
	got engine.Event
	err error
}

func (f *fakeHandler) Handle(_ context.Context, e engine.Event) error {
	f.got = e
	return f.err
}

func TestPostEvents(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		handlerErr error
		wantStatus int
	}{
		{"valid event", `{"source":"alexa","name":"GoodNight","payload":{"room":"bedroom"}}`, nil, http.StatusNoContent},
		{"invalid json", `{`, nil, http.StatusBadRequest},
		{"unknown field", `{"source":"a","name":"b","x":1}`, nil, http.StatusBadRequest},
		{"missing name", `{"source":"alexa"}`, nil, http.StatusBadRequest},
		{"action failure", `{"source":"a","name":"b"}`, errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh := &fakeHandler{err: tt.handlerErr}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := server.New(fh, log)

			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHealthz(t *testing.T) {
	h := server.New(&fakeHandler{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
