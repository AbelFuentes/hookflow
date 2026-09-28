package engine_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

type fakeAction struct {
	calls int
	err   error
}

func (f *fakeAction) Run(context.Context, engine.Event) error {
	f.calls++
	return f.err
}

func newLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestHandle_RunsMatchingRulesOnly(t *testing.T) {
	hit, miss := &fakeAction{}, &fakeAction{}
	en := engine.New(newLogger(),
		engine.Rule{ID: "a", Source: "alexa", Name: "GoodNight", Actions: []engine.Action{hit}},
		engine.Rule{ID: "b", Source: "webhook", Name: "other", Actions: []engine.Action{miss}},
	)

	if err := en.Handle(context.Background(), engine.Event{Source: "alexa", Name: "GoodNight"}); err != nil {
		t.Fatal(err)
	}
	if hit.calls != 1 || miss.calls != 0 {
		t.Fatalf("hit=%d miss=%d", hit.calls, miss.calls)
	}
}

func TestHandle_ContinuesAfterActionError(t *testing.T) {
	boom, ok := &fakeAction{err: errors.New("boom")}, &fakeAction{}
	en := engine.New(newLogger(),
		engine.Rule{ID: "a", Source: "s", Name: "n", Actions: []engine.Action{boom, ok}},
	)

	if err := en.Handle(context.Background(), engine.Event{Source: "s", Name: "n"}); err == nil {
		t.Fatal("expected error")
	}
	if ok.calls != 1 {
		t.Fatal("second action should still run")
	}
}
