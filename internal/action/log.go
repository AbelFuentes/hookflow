package action

import (
	"context"
	"log/slog"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

type logAction struct {
	log     *slog.Logger
	message string
}

func newLog(log *slog.Logger) Factory {
	return func(params map[string]any) (engine.Action, error) {
		msg, ok := params["message"].(string)
		if !ok || msg == "" {
			return nil, errMessageRequired
		}
		return &logAction{log: log, message: msg}, nil
	}
}

func (a *logAction) Run(ctx context.Context, e engine.Event) error {
	a.log.InfoContext(ctx, a.message, "source", e.Source, "event", e.Name)
	return nil
}
