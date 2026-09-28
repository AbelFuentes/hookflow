package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/AbelFuentes/hookflow/internal/engine"
)

const (
	defaultTimeout = 5 * time.Second
	defaultRetries = 3
	defaultBackoff = 200 * time.Millisecond
	maxBackoff     = 10 * time.Second
	maxRetries     = 10
	maxDrainBytes  = 1 << 20
)

var (
	httpMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	httpParams  = []string{"url", "method", "headers", "body", "timeout", "retries", "backoff"}
)

type httpAction struct {
	client  *http.Client
	log     *slog.Logger
	method  string
	url     string
	headers map[string]string
	body    string // vacío = se envía el evento serializado como JSON
	timeout time.Duration
	retries int
	backoff time.Duration
	sleep   func(context.Context, time.Duration) error
}

func newHTTP(log *slog.Logger) Factory {
	return func(params map[string]any) (engine.Action, error) {
		return buildHTTP(log, params)
	}
}

func buildHTTP(log *slog.Logger, params map[string]any) (*httpAction, error) {
	for k := range params {
		if !slices.Contains(httpParams, k) {
			return nil, fmt.Errorf("http: unknown param %q", k)
		}
	}

	rawURL, _ := params["url"].(string)
	u, err := url.Parse(rawURL)
	if rawURL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("http: url must be a valid http(s) URL, got %q", rawURL)
	}

	method := http.MethodPost
	if v, ok := params["method"]; ok {
		s, _ := v.(string)
		method = strings.ToUpper(s)
		if !slices.Contains(httpMethods, method) {
			return nil, fmt.Errorf("http: unsupported method %q", s)
		}
	}

	headers, err := headersParam(params["headers"])
	if err != nil {
		return nil, err
	}

	body := ""
	if v, ok := params["body"]; ok {
		if body, ok = v.(string); !ok {
			return nil, fmt.Errorf("http: body must be a string")
		}
	}

	timeout, err := durationParam(params, "timeout", defaultTimeout)
	if err != nil {
		return nil, err
	}
	backoff, err := durationParam(params, "backoff", defaultBackoff)
	if err != nil {
		return nil, err
	}
	retries, err := retriesParam(params)
	if err != nil {
		return nil, err
	}

	return &httpAction{
		client:  &http.Client{},
		log:     log,
		method:  method,
		url:     rawURL,
		headers: headers,
		body:    body,
		timeout: timeout,
		retries: retries,
		backoff: backoff,
		sleep:   sleepCtx,
	}, nil
}

func (a *httpAction) Run(ctx context.Context, e engine.Event) error {
	payload, err := a.payload(e)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= a.retries; attempt++ {
		if attempt > 0 {
			if err := a.sleep(ctx, a.delay(attempt)); err != nil {
				return err
			}
		}
		retry, err := a.do(ctx, payload)
		if err == nil {
			a.log.InfoContext(ctx, "http action ok", "url", a.url, "attempt", attempt+1)
			return nil
		}
		lastErr = err
		if !retry {
			return fmt.Errorf("http %s %s: %w", a.method, a.url, err)
		}
		a.log.WarnContext(ctx, "http action failed, will retry",
			"url", a.url, "attempt", attempt+1, "err", err)
	}
	return fmt.Errorf("http %s %s: giving up after %d attempts: %w",
		a.method, a.url, a.retries+1, lastErr)
}

// do ejecuta un intento. El bool indica si vale la pena reintentar.
func (a *httpAction) do(ctx context.Context, payload []byte) (bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	var body io.Reader
	if a.method != http.MethodGet {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(reqCtx, a.method, a.url, body)
	if err != nil {
		return false, err
	}
	if a.body == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return ctx.Err() == nil, err // si el contexto padre murió, no reintentar
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("unexpected status %d", resp.StatusCode)
	default:
		return false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

type eventJSON struct {
	Source  string         `json:"source"`
	Name    string         `json:"name"`
	Payload map[string]any `json:"payload,omitempty"`
}

func (a *httpAction) payload(e engine.Event) ([]byte, error) {
	if a.body != "" {
		return []byte(a.body), nil
	}
	return json.Marshal(eventJSON{Source: e.Source, Name: e.Name, Payload: e.Payload})
}

// delay: backoff exponencial con jitter (entre 50% y 100% del valor).
func (a *httpAction) delay(attempt int) time.Duration {
	d := a.backoff << (attempt - 1)
	if d > maxBackoff || d <= 0 {
		d = maxBackoff
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func headersParam(v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("http: headers must be a map")
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("http: header %q must be a string", k)
		}
		out[k] = s
	}
	return out, nil
}

func durationParam(params map[string]any, key string, def time.Duration) (time.Duration, error) {
	v, ok := params[key]
	if !ok {
		return def, nil
	}
	s, _ := v.(string)
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("http: %s must be a positive duration like \"5s\", got %v", key, v)
	}
	return d, nil
}

func retriesParam(params map[string]any) (int, error) {
	v, ok := params["retries"]
	if !ok {
		return defaultRetries, nil
	}
	n, ok := v.(int)
	if !ok || n < 0 || n > maxRetries {
		return 0, fmt.Errorf("http: retries must be an integer between 0 and %d, got %v", maxRetries, v)
	}
	return n, nil
}
