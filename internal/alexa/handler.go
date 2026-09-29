package alexa

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

const (
	maxBodyBytes       = 1 << 20
	timestampTolerance = 150 * time.Second
	engineDeadline     = 6 * time.Second // Alexa espera respuesta en ~8s
)

type RequestVerifier interface {
	Verify(ctx context.Context, certURL, signature string, body []byte) error
}

type EventHandler interface {
	Handle(ctx context.Context, e engine.Event) error
}

type Handler struct {
	skillID  string
	verifier RequestVerifier
	events   EventHandler
	log      *slog.Logger
	now      func() time.Time
}

func NewHandler(skillID string, v RequestVerifier, events EventHandler, log *slog.Logger) *Handler {
	return &Handler{skillID: skillID, verifier: v, events: events, log: log, now: time.Now}
}

type skillRequest struct {
	Context struct {
		System struct {
			Application struct {
				ApplicationID string `json:"applicationId"`
			} `json:"application"`
		} `json:"System"`
	} `json:"context"`
	Request struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Intent    struct {
			Name  string `json:"name"`
			Slots map[string]struct {
				Value string `json:"value"`
			} `json:"slots"`
		} `json:"intent"`
	} `json:"request"`
}

type skillResponse struct {
	Version  string `json:"version"`
	Response struct {
		OutputSpeech     *outputSpeech `json:"outputSpeech,omitempty"`
		ShouldEndSession bool          `json:"shouldEndSession"`
	} `json:"response"`
}

type outputSpeech struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	err = h.verifier.Verify(r.Context(),
		r.Header.Get("SignatureCertChainUrl"), r.Header.Get("Signature-256"), body)
	if err != nil {
		h.log.WarnContext(r.Context(), "alexa request rejected", "err", err)
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	var req skillRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if d := h.now().Sub(req.Request.Timestamp); d > timestampTolerance || d < -timestampTolerance {
		http.Error(w, "stale request", http.StatusBadRequest)
		return
	}
	if req.Context.System.Application.ApplicationID != h.skillID {
		h.log.WarnContext(r.Context(), "alexa request for a different skill")
		http.Error(w, "unknown skill", http.StatusBadRequest)
		return
	}

	event, ok := toEvent(req)
	if !ok {
		writeResponse(w, "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), engineDeadline)
	defer cancel()
	if err := h.events.Handle(ctx, event); err != nil {
		h.log.ErrorContext(ctx, "alexa event failed", "event", event.Name, "err", err)
		writeResponse(w, "Algo falló.")
		return
	}
	writeResponse(w, "Listo.")
}

func toEvent(req skillRequest) (engine.Event, bool) {
	switch req.Request.Type {
	case "LaunchRequest":
		return engine.Event{Source: "alexa", Name: "Launch"}, true
	case "IntentRequest":
		payload := make(map[string]any, len(req.Request.Intent.Slots))
		for name, s := range req.Request.Intent.Slots {
			if s.Value != "" {
				payload[name] = s.Value
			}
		}
		return engine.Event{Source: "alexa", Name: req.Request.Intent.Name, Payload: payload}, true
	default:
		return engine.Event{}, false
	}
}

func writeResponse(w http.ResponseWriter, speech string) {
	var resp skillResponse
	resp.Version = "1.0"
	resp.Response.ShouldEndSession = true
	if speech != "" {
		resp.Response.OutputSpeech = &outputSpeech{Type: "PlainText", Text: speech}
	}
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	_ = json.NewEncoder(w).Encode(resp)
}
