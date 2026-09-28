package action

import (
	"errors"
	"log/slog"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

// Factory construye una Action a partir de los params del YAML.
type Factory func(params map[string]any) (engine.Action, error)

// Registry mapea el "type" del YAML a su Factory.
type Registry map[string]Factory

func DefaultRegistry(log *slog.Logger) Registry {
	return Registry{
		"log":  newLog(log),
		"http": newHTTP(log),
	}
}

var errMessageRequired = errors.New("log: message is required")
