package engine

import "context"

// An event is what triggers rules: Alexa, webhook, cron...
type Event struct {
	Source  string
	Name    string
	Payload map[string]any
}

type Action interface {
	Run(ctx context.Context, e Event) error
}

type Rule struct {
	ID      string
	Source  string
	Name    string
	Actions []Action
}
