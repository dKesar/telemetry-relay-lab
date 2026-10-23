// Package model separates reported observations from physical failure events.
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"

	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

// Probabilities are decimal or rational strings to avoid rounding in exact calculations.
type Group struct {
	ID            string `json:"id"`
	UpProbability string `json:"up_probability"`
}

type Relay struct {
	Observed            selector.Candidate `json:"observed"`
	ConditionalDelivery string             `json:"conditional_delivery"`
	ActualGroups        []string           `json:"actual_groups"`
}

type Scenario struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Options     selector.Options `json:"options"`
	Groups      []Group          `json:"groups"`
	Relays      []Relay          `json:"relays"`
}

type Suite struct {
	SchemaVersion int        `json:"schema_version"`
	Scenarios     []Scenario `json:"scenarios"`
}

func Probability(s string) (*big.Rat, error) {
	p, ok := new(big.Rat).SetString(s)
	if !ok || p.Sign() < 0 || p.Cmp(big.NewRat(1, 1)) > 0 {
		return nil, fmt.Errorf("invalid probability %q", s)
	}
	return p, nil
}

func (s Scenario) Candidates() []selector.Candidate {
	out := make([]selector.Candidate, len(s.Relays))
	for i, r := range s.Relays {
		out[i] = r.Observed
	}
	return out
}

// Validate bounds exact enumeration and simulation work as well as checking inputs.
func (s Scenario) Validate() error {
	if s.Name == "" || len(s.Relays) == 0 {
		return fmt.Errorf("scenario requires name and relays")
	}
	if len(s.Groups)+len(s.Relays) > 16 {
		return fmt.Errorf("at most 16 primary events are supported in v0")
	}
	if _, err := selector.Eligible(s.Candidates(), s.Options); err != nil {
		return err
	}
	groups := make(map[string]bool)
	for _, g := range s.Groups {
		if g.ID == "" || groups[g.ID] {
			return fmt.Errorf("empty or duplicate physical group %q", g.ID)
		}
		groups[g.ID] = true
		if _, err := Probability(g.UpProbability); err != nil {
			return err
		}
	}
	for _, r := range s.Relays {
		if _, err := Probability(r.ConditionalDelivery); err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, g := range r.ActualGroups {
			if !groups[g] || seen[g] {
				return fmt.Errorf("relay %q has unknown or repeated physical group %q", r.Observed.ID, g)
			}
			seen[g] = true
		}
	}
	return nil
}

func Load(path string) (Suite, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, nil, err
	}
	var suite Suite
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&suite); err != nil {
		return Suite{}, nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Suite{}, nil, fmt.Errorf("expected one JSON object")
	}
	if suite.SchemaVersion != 1 || len(suite.Scenarios) == 0 {
		return Suite{}, nil, fmt.Errorf("expected schema_version 1 and non-empty scenarios")
	}
	names := make(map[string]bool)
	for _, s := range suite.Scenarios {
		if names[s.Name] {
			return Suite{}, nil, fmt.Errorf("duplicate scenario %q", s.Name)
		}
		names[s.Name] = true
		if err := s.Validate(); err != nil {
			return Suite{}, nil, fmt.Errorf("scenario %q: %w", s.Name, err)
		}
	}
	return suite, data, nil
}

type World struct {
	Probabilities []*big.Rat
	// Required event indices for successful delivery through each relay.
	Required [][]int
}

func (s Scenario) World() World {
	w := World{Required: make([][]int, len(s.Relays))}
	groups := make(map[string]int)
	for _, g := range s.Groups {
		groups[g.ID] = len(w.Probabilities)
		p, _ := Probability(g.UpProbability)
		w.Probabilities = append(w.Probabilities, p)
	}
	for i, r := range s.Relays {
		w.Required[i] = append(w.Required[i], len(w.Probabilities))
		p, _ := Probability(r.ConditionalDelivery)
		w.Probabilities = append(w.Probabilities, p)
		for _, g := range r.ActualGroups {
			w.Required[i] = append(w.Required[i], groups[g])
		}
	}
	return w
}

func (w World) Outcomes(events []bool) []bool {
	out := make([]bool, len(w.Required))
	for i, required := range w.Required {
		out[i] = true
		for _, index := range required {
			if !events[index] {
				out[i] = false
				break
			}
		}
	}
	return out
}
