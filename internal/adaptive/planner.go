// Package adaptive selects a bounded replica set using observed joint outcomes.
// It has no dependency on the simulator or its physical failure schedule.
package adaptive

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
)

const Revision = "adaptive-set-v1"
const Window = 24
const PriorWeight = 4.0
const CopyCost = .025

type Identity struct {
	ID    string
	Prior float64
}
type Node struct {
	ID                 string
	Groups             []string
	Known, Ready       bool
	Queue              float64
	FreeBytes          int64
	LocalMS, ServiceMS float64
}
type Options struct {
	Budget                          int
	Bytes                           int64
	RemainingMS, SourceQueue        float64
	Independent, NoDeadline, Static bool // controlled ablations, not separate tuned algorithms
}
type Assessment struct {
	ID       string  `json:"id"`
	Estimate float64 `json:"estimate"`
	ETA      float64 `json:"eta_ms"`
	Gain     float64 `json:"gain"`
	Cost     float64 `json:"cost"`
}
type Plan struct {
	Chosen      []string     `json:"chosen"`
	Expected    float64      `json:"expected"`
	Utility     float64      `json:"utility"`
	Evaluated   int          `json:"evaluated"`
	Assessments []Assessment `json:"assessments"`
}
type Health struct {
	ID          string  `json:"id"`
	Estimate    float64 `json:"estimate"`
	Samples     int     `json:"samples"`
	Informative int     `json:"informative"`
}
type Pair struct {
	A       string  `json:"a"`
	B       string  `json:"b"`
	Phi     float64 `json:"phi"`
	Samples int     `json:"samples"`
}
type sample struct {
	time int64
	up   uint16
}
type Learner struct {
	ids     []Identity
	index   map[string]int
	history []sample
	last    int64
}

func unit(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }
func New(ids []Identity) (*Learner, error) {
	if len(ids) < 1 || len(ids) > 12 {
		return nil, fmt.Errorf("adaptive planner requires 1..12 identities")
	}
	l := &Learner{ids: append([]Identity(nil), ids...), index: map[string]int{}, last: -1}
	sort.Slice(l.ids, func(i, j int) bool { return l.ids[i].ID < l.ids[j].ID })
	for i, v := range l.ids {
		if v.ID == "" || !unit(v.Prior) {
			return nil, fmt.Errorf("invalid identity")
		}
		if _, ok := l.index[v.ID]; ok {
			return nil, fmt.Errorf("duplicate identity")
		}
		l.index[v.ID] = i
	}
	return l, nil
}

// Observe receives only completed probes. An all-zero observation is retained
// for diagnostics but gives no evidence for preferring one relay over another.
func (l *Learner) Observe(at int64, up map[string]bool) error {
	if at < 0 || at <= l.last || len(up) != len(l.ids) {
		return fmt.Errorf("invalid observation time or identities")
	}
	var mask uint16
	for _, id := range l.ids {
		v, ok := up[id.ID]
		if !ok {
			return fmt.Errorf("missing probe outcome")
		}
		if v {
			mask |= 1 << l.index[id.ID]
		}
	}
	l.history = append(l.history, sample{at, mask})
	if len(l.history) > Window {
		l.history = append([]sample(nil), l.history[len(l.history)-Window:]...)
	}
	l.last = at
	return nil
}
func (l *Learner) informative() []sample {
	var out []sample
	for _, s := range l.history {
		if s.up != 0 {
			out = append(out, s)
		}
	}
	return out
}
func (l *Learner) marginal(i int, samples []sample) float64 {
	s := PriorWeight * l.ids[i].Prior
	for _, v := range samples {
		if v.up&(1<<i) != 0 {
			s++
		}
	}
	return s / (PriorWeight + float64(len(samples)))
}
func (l *Learner) Health() []Health {
	out := make([]Health, 0, len(l.ids))
	n := float64(len(l.history))
	info := l.informative()
	for i, id := range l.ids {
		out = append(out, Health{id.ID, l.marginal(i, info), int(n), len(info)})
	}
	return out
}
func (l *Learner) Pairs() []Pair {
	var out []Pair
	for i := range l.ids {
		for j := i + 1; j < len(l.ids); j++ {
			a, b, c, d := 0., 0., 0., 0.
			for _, s := range l.history {
				x, y := s.up&(1<<i) != 0, s.up&(1<<j) != 0
				switch {
				case x && y:
					a++
				case x:
					b++
				case y:
					c++
				default:
					d++
				}
			}
			den := math.Sqrt((a + b) * (c + d) * (a + c) * (b + d))
			phi := 0.
			if den > 0 {
				phi = (a*d - b*c) / den
			}
			out = append(out, Pair{l.ids[i].ID, l.ids[j].ID, phi, len(l.history)})
		}
	}
	return out
}

// Choose enumerates at most 2^12 sets and returns the exact maximum of this
// declared utility. That is not a claim of optimal routing in the real network.
func (l *Learner) Choose(now int64, nodes []Node, existing []string, o Options) (Plan, error) {
	if now < l.last || len(nodes) != len(l.ids) || o.Budget < 0 || o.Budget > len(nodes) || o.Bytes <= 0 || !unit(o.SourceQueue) || math.IsNaN(o.RemainingMS) || math.IsInf(o.RemainingMS, 0) || o.RemainingMS <= 0 {
		return Plan{}, fmt.Errorf("invalid planning inputs or future observation")
	}
	n := len(nodes)
	ordered := make([]Node, n)
	seen := map[string]bool{}
	for _, v := range nodes {
		i, ok := l.index[v.ID]
		if !ok || seen[v.ID] || !unit(v.Queue) || v.FreeBytes < 0 || v.LocalMS < 0 || v.ServiceMS < 0 || math.IsNaN(v.LocalMS+v.ServiceMS) || math.IsInf(v.LocalMS+v.ServiceMS, 0) {
			return Plan{}, fmt.Errorf("invalid node")
		}
		seen[v.ID] = true
		ordered[i] = v
	}
	var old, allowed uint16
	for _, id := range existing {
		i, ok := l.index[id]
		if !ok || old&(1<<i) != 0 {
			return Plan{}, fmt.Errorf("invalid ledger")
		}
		old |= 1 << i
	}
	if bits.OnesCount16(old) > o.Budget {
		return Plan{}, fmt.Errorf("ledger exceeds budget")
	}
	info := l.informative()
	if o.Static {
		info = nil
	}
	feasible := make([]float64, n)
	cost := make([]float64, n)
	p := make([]float64, n)
	for i, v := range ordered {
		eta := v.LocalMS + v.ServiceMS
		feasible[i] = math.Max(0, math.Min(1, 1-eta/o.RemainingMS))
		if o.NoDeadline {
			feasible[i] = 1
		}
		p[i] = l.marginal(i, info)
		cost[i] = CopyCost * (1 + o.SourceQueue + v.Queue) / math.Max(.2, p[i])
		if old&(1<<i) == 0 && v.Ready && v.FreeBytes >= o.Bytes && v.LocalMS <= o.RemainingMS && feasible[i] > 0 {
			allowed |= 1 << i
		}
	}
	// Connected components of reported labels form a conservative cold-start
	// prior. Its weight decreases as joint observations arrive.
	parents := make([]int, n)
	for i := range parents {
		parents[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		if parents[i] != i {
			parents[i] = root(parents[i])
		}
		return parents[i]
	}
	for i := range ordered {
		for j := i + 1; j < n; j++ {
			overlap := !ordered[i].Known && !ordered[j].Known
			for _, a := range ordered[i].Groups {
				for _, b := range ordered[j].Groups {
					overlap = overlap || a == b
				}
			}
			if overlap {
				parents[root(j)] = root(i)
			}
		}
	}
	predict := func(mask uint16) float64 {
		if mask == 0 {
			return 0
		}
		indFail := 1.
		var groupMax [12]float64 // validated bound; avoid one heap allocation per set
		for i := range ordered {
			if mask&(1<<i) == 0 {
				continue
			}
			v := l.ids[i].Prior * feasible[i]
			indFail *= 1 - v
			groupMax[root(i)] = math.Max(groupMax[root(i)], v)
		}
		groupFail := 1.
		for _, v := range groupMax[:n] {
			groupFail *= 1 - v
		}
		prior := .5*(1-indFail) + .5*(1-groupFail)
		if o.Independent {
			f := 1.
			for i := range ordered {
				if mask&(1<<i) != 0 {
					f *= 1 - p[i]*feasible[i]
				}
			}
			return 1 - f
		}
		sum := PriorWeight * prior
		for _, s := range info {
			fail := 1.
			for i := range ordered {
				if mask&(1<<i) != 0 && s.up&(1<<i) != 0 {
					fail *= 1 - feasible[i]
				}
			}
			sum += 1 - fail
		}
		return sum / (PriorWeight + float64(len(info)))
	}
	best := old
	bestUtility := math.Inf(-1)
	evaluated := 0
	for sub := allowed; ; sub = (sub - 1) & allowed {
		mask := old | sub
		if bits.OnesCount16(mask) <= o.Budget && (old != 0 || sub != 0 || allowed == 0 || o.Budget == 0) {
			u := predict(mask)
			for i := range ordered {
				if sub&(1<<i) != 0 {
					u -= cost[i]
				}
			}
			evaluated++
			if u > bestUtility+1e-12 || (math.Abs(u-bestUtility) <= 1e-12 && (bits.OnesCount16(mask) < bits.OnesCount16(best) || (bits.OnesCount16(mask) == bits.OnesCount16(best) && mask < best))) {
				best, bestUtility = mask, u
			}
		}
		if sub == 0 {
			break
		}
	}
	result := Plan{Chosen: []string{}, Expected: predict(best), Utility: bestUtility, Evaluated: evaluated, Assessments: []Assessment{}}
	base := predict(old)
	for i, v := range ordered {
		if allowed&(1<<i) != 0 {
			result.Assessments = append(result.Assessments, Assessment{v.ID, p[i], v.LocalMS + v.ServiceMS, predict(old|1<<i) - base, cost[i]})
		}
		if best&(1<<i) != 0 && old&(1<<i) == 0 {
			result.Chosen = append(result.Chosen, v.ID)
		}
	}
	return result, nil
}
