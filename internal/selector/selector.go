// Package selector chooses a bounded set of recipients from an observed snapshot.
// It never receives the simulated physical failure outcomes.
package selector

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

type Policy string

const (
	Random       Policy = "random"
	Availability Policy = "availability"
	Diverse      Policy = "diverse"
	RiskAware    Policy = "risk_aware"
	All          Policy = "all"
)

func Policies() []Policy { return []Policy{Random, Availability, Diverse, RiskAware, All} }

// Availability is the historical score before age decay, not a calibrated
// probability. RiskKnown=false denotes incomplete labels, not an independent node.
type Candidate struct {
	ID              string   `json:"id"`
	Availability    float64  `json:"availability"`
	QueueFraction   float64  `json:"queue_fraction"`
	FreeBytes       int64    `json:"free_bytes"`
	TransferMillis  float64  `json:"transfer_millis"`
	ReportAgeMillis float64  `json:"report_age_millis"`
	RiskKnown       bool     `json:"risk_known"`
	Risks           []string `json:"risks"`
}

type Options struct {
	Budget         int     `json:"budget"`
	MessageBytes   int64   `json:"message_bytes"`
	DeadlineMillis float64 `json:"deadline_millis"`
	Lambda         float64 `json:"lambda"`
	UnknownPenalty float64 `json:"unknown_penalty"`
}

type Decision struct {
	ID    string
	Score float64
	Risk  float64
}

func unit(x float64) bool        { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 && x <= 1 }
func nonnegative(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 }

// HistoricalScore uses Laplace smoothing: no observations gives 0.5.
func HistoricalScore(successes, observed int) (float64, error) {
	if successes < 0 || observed < 0 || successes > observed {
		return 0, fmt.Errorf("invalid history: successes=%d observed=%d", successes, observed)
	}
	return (float64(successes) + 1) / (float64(observed) + 2), nil
}

// Eligible validates all inputs and returns a sorted copy; callers' slices are not changed.
// Labels can overlap across several domains (access point, power, segment).
func Eligible(candidates []Candidate, opts Options) ([]Candidate, error) {
	if opts.Budget < 0 || opts.MessageBytes <= 0 || !nonnegative(opts.DeadlineMillis) || opts.DeadlineMillis == 0 || !nonnegative(opts.Lambda) || !unit(opts.UnknownPenalty) {
		return nil, fmt.Errorf("invalid selector options")
	}
	seen := make(map[string]bool)
	var result []Candidate
	for _, c := range candidates {
		if c.ID == "" || seen[c.ID] {
			return nil, fmt.Errorf("empty or duplicate candidate ID %q", c.ID)
		}
		seen[c.ID] = true
		if !unit(c.Availability) || !unit(c.QueueFraction) || c.FreeBytes < 0 || !nonnegative(c.TransferMillis) || !nonnegative(c.ReportAgeMillis) {
			return nil, fmt.Errorf("invalid candidate %q", c.ID)
		}
		labels := make(map[string]bool)
		for _, label := range c.Risks {
			if label == "" || labels[label] {
				return nil, fmt.Errorf("invalid risk label for %q", c.ID)
			}
			labels[label] = true
		}
		if c.FreeBytes >= opts.MessageBytes && c.TransferMillis <= opts.DeadlineMillis && c.ReportAgeMillis <= 60000 {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func baseScore(c Candidate) float64 {
	return c.Availability * math.Exp2(-c.ReportAgeMillis/30000) / (1 + c.QueueFraction)
}

func overlap(a, b Candidate) bool {
	for _, x := range a.Risks {
		for _, y := range b.Risks {
			if x == y {
				return true
			}
		}
	}
	return false
}

// An observed overlap takes precedence over incomplete labels. With no previous
// recipient the risk is zero, even if this candidate's labels are unknown.
func risk(c Candidate, chosen []Candidate, unknown float64) float64 {
	if len(chosen) == 0 {
		return 0
	}
	known := c.RiskKnown
	for _, old := range chosen {
		if overlap(c, old) {
			return 1
		}
		known = known && old.RiskKnown
	}
	if known {
		return 0
	}
	return unknown
}

func Select(candidates []Candidate, opts Options, policy Policy, rng *rand.Rand) ([]Decision, error) {
	pool, err := Eligible(candidates, opts)
	if err != nil {
		return nil, err
	}
	switch policy {
	case Random, Availability, Diverse, RiskAware, All:
	default:
		return nil, fmt.Errorf("unknown policy %q", policy)
	}
	limit := opts.Budget
	if policy == All || limit > len(pool) {
		limit = len(pool)
	}
	if policy == Random {
		if rng == nil {
			return nil, fmt.Errorf("random policy requires an RNG")
		}
		rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		out := make([]Decision, 0, limit)
		for _, c := range pool[:limit] {
			out = append(out, Decision{ID: c.ID, Score: baseScore(c)})
		}
		return out, nil
	}
	var chosen []Candidate
	out := make([]Decision, 0, limit)
	for len(out) < limit {
		best, bestScore, bestRisk := -1, -1.0, 2.0
		for i, c := range pool {
			r := risk(c, chosen, opts.UnknownPenalty)
			s := baseScore(c)
			if policy == RiskAware {
				s /= 1 + opts.Lambda*r
			}
			better := best == -1 || s > bestScore
			if policy == Diverse {
				better = best == -1 || r < bestRisk || (r == bestRisk && s > bestScore)
			}
			// Sorted IDs make exact ties deterministic.
			if better {
				best, bestScore, bestRisk = i, s, r
			}
		}
		c := pool[best]
		chosen = append(chosen, c)
		out = append(out, Decision{ID: c.ID, Score: bestScore, Risk: bestRisk})
		pool = append(pool[:best], pool[best+1:]...)
	}
	return out, nil
}
