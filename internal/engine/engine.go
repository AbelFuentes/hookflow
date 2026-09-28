package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type Engine struct {
	rules []Rule
	log   *slog.Logger
}

func New(log *slog.Logger, rules ...Rule) *Engine {
	return &Engine{rules: rules, log: log}
}

// Handle executes all actions for matching rules.
// A failure does not stop the other actions; errors are aggregated.
func (en *Engine) Handle(ctx context.Context, e Event) error {
	var errs []error
	for _, r := range en.rules {
		if r.Source != e.Source || r.Name != e.Name {
			continue
		}

		en.log.InfoContext(ctx, "rule matched", "rule", r.ID, "event", e.Name)
		for _, a := range r.Actions {
			if err := a.Run(ctx, e); err != nil {
				errs = append(errs, fmt.Errorf("rule %s: %w", r.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}
