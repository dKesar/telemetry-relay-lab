// Package experiment runs snapshot experiments, not a network or queue simulator.
package experiment

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/dKesar/telemetry-relay-lab/internal/model"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

type RunResult struct {
	Policy                   selector.Policy
	Seed                     int64
	Trials                   int
	Delivered                int
	Copies                   int
	PlacementBytesPerMessage int64
}

func (r RunResult) Rate() float64 { return float64(r.Delivered) / float64(r.Trials) }

// Run samples one world per trial and reuses it for every policy. Physical
// events and the random selector use separate RNG streams; adding a policy
// cannot change the world. Each trial is a fresh pre-placed message snapshot.
func Run(s model.Scenario, seed int64, trials int) ([]RunResult, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if trials <= 0 || trials > 10000000 {
		return nil, fmt.Errorf("trials must be between 1 and 10000000")
	}
	if s.Options.MessageBytes > math.MaxInt64/int64(len(s.Relays)) {
		return nil, fmt.Errorf("placement byte count would overflow")
	}
	w := s.World()
	probs := make([]float64, len(w.Probabilities))
	for i, p := range w.Probabilities {
		probs[i], _ = p.Float64()
	}
	physical := rand.New(rand.NewSource(seed))
	choice := rand.New(rand.NewSource(seed ^ 0x5deece66d))
	idIndex := make(map[string]int)
	for i, r := range s.Relays {
		idIndex[r.Observed.ID] = i
	}
	rows := make([]RunResult, 0, 5)
	selected := make([][]int, 0, 5)
	eligible, _ := selector.Eligible(s.Candidates(), s.Options)
	randomPool := make([]int, len(eligible))
	for i, c := range eligible {
		randomPool[i] = idIndex[c.ID]
	}
	for _, policy := range selector.Policies() {
		row := RunResult{Policy: policy, Seed: seed, Trials: trials}
		var indices []int
		if policy == selector.Random {
			row.Copies = min(s.Options.Budget, len(eligible))
		} else {
			d, err := selector.Select(s.Candidates(), s.Options, policy, nil)
			if err != nil {
				return nil, err
			}
			for _, v := range d {
				indices = append(indices, idIndex[v.ID])
			}
			row.Copies = len(indices)
		}
		row.PlacementBytesPerMessage = int64(row.Copies) * s.Options.MessageBytes
		rows = append(rows, row)
		selected = append(selected, indices)
	}
	events := make([]bool, len(probs))
	for trial := 0; trial < trials; trial++ {
		for i, p := range probs {
			events[i] = physical.Float64() < p
		}
		outcomes := w.Outcomes(events)
		for i := range rows {
			indices := selected[i]
			if rows[i].Policy == selector.Random {
				// Shuffle in place gives a fresh uniform K-subset each trial.
				choice.Shuffle(len(randomPool), func(a, b int) { randomPool[a], randomPool[b] = randomPool[b], randomPool[a] })
				indices = randomPool[:rows[i].Copies]
			}
			for _, index := range indices {
				if outcomes[index] {
					rows[i].Delivered++
					break
				}
			}
		}
	}
	return rows, nil
}

type Summary struct {
	Policy selector.Policy
	Mean   float64
	Lower  float64
	Upper  float64
	// Paired difference versus availability on the same run's world.
	DeltaMean  float64
	DeltaLower float64
	DeltaUpper float64
}

// Summarize bootstraps runs, not individual messages. The seed is independent
// of the physical world. With fewer than two runs there is no interval.
func Summarize(runs [][]RunResult, bootstrapSeed int64) ([]Summary, error) {
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs")
	}
	policies := selector.Policies()
	values := make(map[selector.Policy][]float64)
	for _, run := range runs {
		seen := make(map[selector.Policy]bool)
		for _, r := range run {
			if r.Trials <= 0 || r.Delivered < 0 || r.Delivered > r.Trials || seen[r.Policy] {
				return nil, fmt.Errorf("invalid run results")
			}
			seen[r.Policy] = true
			values[r.Policy] = append(values[r.Policy], r.Rate())
		}
		if len(seen) != len(policies) {
			return nil, fmt.Errorf("incomplete run")
		}
		for _, p := range policies {
			if !seen[p] {
				return nil, fmt.Errorf("unknown or missing policy")
			}
		}
	}
	out := make([]Summary, 0, len(policies))
	for _, p := range policies {
		v := values[p]
		delta := make([]float64, len(v))
		for i := range v {
			delta[i] = v[i] - values[selector.Availability][i]
		}
		mean, lo, hi := interval(v, bootstrapSeed)
		dmean, dlo, dhi := interval(delta, bootstrapSeed)
		out = append(out, Summary{Policy: p, Mean: mean, Lower: lo, Upper: hi, DeltaMean: dmean, DeltaLower: dlo, DeltaUpper: dhi})
	}
	return out, nil
}

func interval(values []float64, seed int64) (float64, float64, float64) {
	mean := 0.0
	for _, v := range values {
		mean += v / float64(len(values))
	}
	if len(values) < 2 {
		return mean, math.NaN(), math.NaN()
	}
	rng := rand.New(rand.NewSource(seed))
	const repetitions = 2000
	means := make([]float64, repetitions)
	for i := range means {
		for range values {
			means[i] += values[rng.Intn(len(values))] / float64(len(values))
		}
	}
	sort.Float64s(means)
	return mean, means[49], means[1949]
}
