package experiment

import (
	"math/big"

	"github.com/dKesar/telemetry-relay-lab/internal/model"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

type ExactResult struct {
	Policy      selector.Policy
	Recipients  []string
	Copies      int
	Probability *big.Rat
}

// Exact enumerates independent primary events with rational weights. Delivery
// events of nodes sharing a physical group are correlated, not independent.
// Random is the analytical average over uniform K-subsets of eligible nodes.
func Exact(s model.Scenario) ([]ExactResult, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	eligible, err := selector.Eligible(s.Candidates(), s.Options)
	if err != nil {
		return nil, err
	}
	eligibleIDs := make(map[string]bool)
	for _, c := range eligible {
		eligibleIDs[c.ID] = true
	}
	rows := make([]ExactResult, 0, 5)
	for _, policy := range selector.Policies() {
		row := ExactResult{Policy: policy, Probability: new(big.Rat)}
		if policy == selector.Random {
			row.Copies = min(s.Options.Budget, len(eligible))
		} else {
			d, err := selector.Select(s.Candidates(), s.Options, policy, nil)
			if err != nil {
				return nil, err
			}
			for _, v := range d {
				row.Recipients = append(row.Recipients, v.ID)
			}
			row.Copies = len(d)
		}
		rows = append(rows, row)
	}
	w := s.World()
	events := make([]bool, len(w.Probabilities))
	for state := 0; state < 1<<len(events); state++ {
		weight := big.NewRat(1, 1)
		for i, p := range w.Probabilities {
			events[i] = state&(1<<i) != 0
			factor := p
			if !events[i] {
				factor = new(big.Rat).Sub(big.NewRat(1, 1), p)
			}
			weight.Mul(weight, factor)
		}
		if weight.Sign() == 0 {
			continue
		}
		outcomes := w.Outcomes(events)
		successIDs := make(map[string]bool)
		for i, ok := range outcomes {
			if ok {
				successIDs[s.Relays[i].Observed.ID] = true
			}
		}
		for i := range rows {
			row := &rows[i]
			if row.Policy == selector.Random {
				successful := 0
				for id := range successIDs {
					if eligibleIDs[id] {
						successful++
					}
				}
				hit := uniformHit(len(eligible), successful, row.Copies)
				row.Probability.Add(row.Probability, new(big.Rat).Mul(weight, hit))
				continue
			}
			for _, id := range row.Recipients {
				if successIDs[id] {
					row.Probability.Add(row.Probability, weight)
					break
				}
			}
		}
	}
	return rows, nil
}

func uniformHit(n, successful, k int) *big.Rat {
	if k == 0 || successful == 0 {
		return new(big.Rat)
	}
	if n-successful < k {
		return big.NewRat(1, 1)
	}
	miss := new(big.Rat).SetFrac(new(big.Int).Binomial(int64(n-successful), int64(k)), new(big.Int).Binomial(int64(n), int64(k)))
	return new(big.Rat).Sub(big.NewRat(1, 1), miss)
}
