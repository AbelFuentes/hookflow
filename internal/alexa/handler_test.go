package alexa_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AbelFuentes/hookflow/internal/alexa"
	"github.com/AbelFuentes/hookflow/internal/engine"
)

type fakeVerifier struct{ err error }

func (f fakeVerifier) Verify(context.Context, string, string, []byte) error { return f.err }

type fakeEvents struct {
	got []engine.Event
	err error
}

func (f *fakeEvents) Handle(_ context.Context, e engine.Event) error {
	f.got = append(f.got, e)
	return f.err
}

func skillBody(appID, reqType string, ts time.Time) string {
	return fmt.Sprintf(`{"context":{"System":{"application":{"applicationId":%q}}},`+
		`"request":{"type":%q,"timestamp":%q,"intent":{"name":"GoodNightIntent","slots":`+
		`{"room":{"name":"room","value":"bedroom"},"empty":{"name":"empty"}}}}}`,
		appID, reqType, ts.UTC().Format(time.RFC3339))
}

func serve(v alexa.RequestVerifier, ev *fakeEvents, body string) *httptest.ResponseRecorder {
	h := alexa.NewHandler("amzn1.ask.skill.test", v, ev, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/alexa", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler(t *testing.T) {
	const skill = "amzn1.ask.skill.test"
	now := time.Now()

	tests := []struct {
		name       string
		verifyErr  error
		appID      string
		reqType    string
		ts         time.Time
		actionErr  error
		wantStatus int
		wantEvent  string // "" = ningún evento
		wantSpeech string
	}{
		{"intent", nil, skill, "IntentRequest", now, nil, 200, "GoodNightIntent", "Listo"},
		{"launch", nil, skill, "LaunchRequest", now, nil, 200, "Launch", "Listo"},
		{"session ended", nil, skill, "SessionEndedRequest", now, nil, 200, "", ""},
		{"action failure", nil, skill, "IntentRequest", now, errors.New("boom"), 200, "GoodNightIntent", "falló"},
		{"bad signature", errors.New("bad"), skill, "IntentRequest", now, nil, 400, "", ""},
		{"stale timestamp", nil, skill, "IntentRequest", now.Add(-10 * time.Minute), nil, 400, "", ""},
		{"wrong skill", nil, "amzn1.ask.skill.other", "IntentRequest", now, nil, 400, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &fakeEvents{err: tt.actionErr}
			rec := serve(fakeVerifier{err: tt.verifyErr}, ev, skillBody(tt.appID, tt.reqType, tt.ts))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantEvent == "" {
				if len(ev.got) != 0 {
					t.Fatalf("unexpected events: %+v", ev.got)
				}
			} else if len(ev.got) != 1 || ev.got[0].Source != "alexa" || ev.got[0].Name != tt.wantEvent {
				t.Fatalf("events = %+v, want one %q", ev.got, tt.wantEvent)
			}
			if !strings.Contains(rec.Body.String(), tt.wantSpeech) {
				t.Fatalf("body %q missing %q", rec.Body.String(), tt.wantSpeech)
			}
		})
	}
}

func TestHandler_MapsSlotsToPayload(t *testing.T) {
	ev := &fakeEvents{}
	serve(fakeVerifier{}, ev, skillBody("amzn1.ask.skill.test", "IntentRequest", time.Now()))

	if len(ev.got) != 1 {
		t.Fatalf("events = %d", len(ev.got))
	}
	p := ev.got[0].Payload
	if p["room"] != "bedroom" {
		t.Fatalf("payload = %+v", p)
	}
	if _, ok := p["empty"]; ok {
		t.Fatal("slot without value must be omitted")
	}
}
