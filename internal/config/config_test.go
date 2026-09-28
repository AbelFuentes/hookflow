package config_test

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/AbelFuentes/hookflow/internal/action"
	"github.com/AbelFuentes/hookflow/internal/config"
)

func registry() action.Registry {
	return action.DefaultRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestParse_Valid(t *testing.T) {
	src := `
rules:
  - id: good-night
    on: {source: alexa, name: GoodNight}
    do:
      - type: log
        message: buenas noches
`
	rules, err := config.Parse(strings.NewReader(src), registry())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "good-night" || len(rules[0].Actions) != 1 {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{"unknown field", "rules:\n  - id: a\n    bogus: 1\n", "bogus"},
		{"missing id", "rules:\n  - on: {source: s, name: n}\n    do: [{type: log, message: hi}]\n", "id is required"},
		{"missing trigger", "rules:\n  - id: a\n    do: [{type: log, message: hi}]\n", "on.source and on.name"},
		{"no actions", "rules:\n  - id: a\n    on: {source: s, name: n}\n", "at least one action"},
		{"unknown action", "rules:\n  - id: a\n    on: {source: s, name: n}\n    do: [{type: nope}]\n", "unknown action type"},
		{"bad params", "rules:\n  - id: a\n    on: {source: s, name: n}\n    do: [{type: log}]\n", "message is required"},
		{"duplicate id", "rules:\n  - {id: a, on: {source: s, name: n}, do: [{type: log, message: x}]}\n  - {id: a, on: {source: s, name: n}, do: [{type: log, message: x}]}\n", "duplicate id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse(strings.NewReader(tt.src), registry())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
