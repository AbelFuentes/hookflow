package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// EventHandler decouples the server from the engine (facilitates testing).
type EventHandler interface {
	Handle(ctx context.Context, e engine.Event) error
}

type eventRequest struct {
	Source  string         `json:"source"`
	Name    string         `json:"name"`
	Payload map[string]any `json:"payload"`
}

func New(h EventHandler, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /events", postEvent(h, log))
	return mux
}

func postEvent(h EventHandler, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		var req eventRequest
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if req.Source == "" || req.Name == "" {
			http.Error(w, "source and name are required", http.StatusBadRequest)
			return
		}

		err := h.Handle(r.Context(), engine.Event{
			Source:  req.Source,
			Name:    req.Name,
			Payload: req.Payload,
		})
		if err != nil {
			log.ErrorContext(r.Context(), "event handling failed", "source", req.Source, "name", req.Name, "err", err)
			http.Error(w, "one or more actions failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
