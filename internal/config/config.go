package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/AbelFuentes/hookflow/internal/action"
	"github.com/AbelFuentes/hookflow/internal/engine"
)

type file struct {
	Rules []ruleSpec `yaml:"rules"`
}

type ruleSpec struct {
	ID string `yaml:"id"`
	On struct {
		Source string `yaml:"source"`
		Name   string `yaml:"name"`
	} `yaml:"on"`
	Do []actionSpec `yaml:"do"`
}

type actionSpec struct {
	Type   string         `yaml:"type"`
	Params map[string]any `yaml:",inline"`
}

func Load(path string, reg action.Registry) ([]engine.Rule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rules: %w", err)
	}
	defer f.Close()
	return Parse(f, reg)
}

func Parse(r io.Reader, reg action.Registry) ([]engine.Rule, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	var errs []error
	seen := make(map[string]struct{}, len(f.Rules))
	rules := make([]engine.Rule, 0, len(f.Rules))

	for i, rs := range f.Rules {
		if _, dup := seen[rs.ID]; dup && rs.ID != "" {
			errs = append(errs, fmt.Errorf("rule %q: duplicate id", rs.ID))
			continue
		}
		seen[rs.ID] = struct{}{}

		rule, err := buildRule(i, rs, reg)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rules = append(rules, rule)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return rules, nil
}

func buildRule(idx int, rs ruleSpec, reg action.Registry) (engine.Rule, error) {
	label := fmt.Sprintf("rules[%d]", idx)
	if rs.ID != "" {
		label = fmt.Sprintf("rule %q", rs.ID)
	}

	var errs []error
	if rs.ID == "" {
		errs = append(errs, fmt.Errorf("%s: id is required", label))
	}
	if rs.On.Source == "" || rs.On.Name == "" {
		errs = append(errs, fmt.Errorf("%s: on.source and on.name are required", label))
	}
	if len(rs.Do) == 0 {
		errs = append(errs, fmt.Errorf("%s: do must have at least one action", label))
	}

	actions := make([]engine.Action, 0, len(rs.Do))
	for j, spec := range rs.Do {
		factory, ok := reg[spec.Type]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: do[%d]: unknown action type %q", label, j, spec.Type))
			continue
		}
		a, err := factory(spec.Params)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: do[%d]: %w", label, j, err))
			continue
		}
		actions = append(actions, a)
	}

	if err := errors.Join(errs...); err != nil {
		return engine.Rule{}, err
	}
	return engine.Rule{
		ID:      rs.ID,
		Source:  rs.On.Source,
		Name:    rs.On.Name,
		Actions: actions,
	}, nil
}
