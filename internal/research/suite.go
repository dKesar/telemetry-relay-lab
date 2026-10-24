// Package research produces paired, reproducible evidence for declared cases.
package research

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/dKesar/telemetry-relay-lab/internal/adaptive"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

const Protocol = "paired-jitter-v1"

func orderRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Case != rows[j].Case {
			return rows[i].Case < rows[j].Case
		}
		if rows[i].Policy != rows[j].Policy {
			return rows[i].Policy < rows[j].Policy
		}
		return rows[i].Run < rows[j].Run
	})
}

type Row struct {
	Case   string          `json:"case"`
	Run    int             `json:"run"`
	Seed   int64           `json:"seed"`
	Policy selector.Policy `json:"policy"`
	Stats  sim.Stats       `json:"stats"`
}
type Estimate struct {
	Mean float64 `json:"mean"`
	Low  float64 `json:"ci95_low"`
	High float64 `json:"ci95_high"`
}
type Summary struct {
	Case        string          `json:"case"`
	Policy      selector.Policy `json:"policy"`
	Runs        int             `json:"runs"`
	Delivery    Estimate        `json:"delivery"`
	Bytes       Estimate        `json:"bytes"`
	P95Mean     float64         `json:"mean_run_p95_ms"`
	LatencyRuns int             `json:"runs_with_delivery"`
	CopiesMean  float64         `json:"mean_copies_per_generated"`
}
type Comparison struct {
	Case          string          `json:"case"`
	Baseline      selector.Policy `json:"baseline"`
	DeliveryDelta Estimate        `json:"delivery_delta"`
	ByteSaving    Estimate        `json:"relative_byte_saving"`
	Decision      string          `json:"interpretation"`
}
type Report struct {
	Protocol    string       `json:"protocol"`
	Model       string       `json:"model"`
	Algorithm   string       `json:"algorithm"`
	Seed        int64        `json:"base_seed"`
	Runs        int          `json:"runs"`
	Checks      int          `json:"validated_runs"`
	Summary     []Summary    `json:"summary"`
	Comparisons []Comparison `json:"comparisons"`
}

func Cases() []sim.Config {
	out := sim.Presets()
	for _, ttl := range []int64{2000, 4000} {
		c, _ := sim.Preset("independent")
		c.Name = fmt.Sprintf("ttl_%dms", ttl)
		c.Title = c.Name
		c.TTLMS = ttl
		out = append(out, c)
	}
	for _, hz := range []int64{8, 12} {
		c, _ := sim.Preset("overload")
		c.Name = fmt.Sprintf("load_%dhz", hz)
		c.Title = c.Name
		c.IntervalMS = 1000 / hz
		out = append(out, c)
	}
	return out
}

// Variant perturbs the physical world before running any policy. No parameter
// fitting uses these held-out seeds. Each policy receives the identical config.
func Variant(base sim.Config, seed int64) sim.Config {
	data, _ := json.Marshal(base)
	var c sim.Config
	_ = json.Unmarshal(data, &c)
	h := sha256.Sum256([]byte(c.Name))
	r := rand.New(rand.NewSource(seed ^ int64(h[0])<<40 ^ 0x243f6a8885a308d3))
	clamp := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	for i := range c.Relays {
		c.Relays[i].DropProbability = clamp(c.Relays[i].DropProbability + (r.Float64()-.5)*.08)
		for j := range c.Relays[i].Phases {
			c.Relays[i].Phases[j].DropProbability = clamp(c.Relays[i].Phases[j].DropProbability + (r.Float64()-.5)*.04)
		}
	}
	for i := range c.Domains {
		for j := range c.Domains[i].Outages {
			x := &c.Domains[i].Outages[j]
			shift := int64(r.Intn(1001)) - 500
			x.Start = max(0, x.Start+shift)
			x.End = min(c.DurationMS, x.End+shift)
		}
	}
	c.Seed = seed
	return c
}

// ValidateResult checks independently from the planner and the UI.
func ValidateResult(c sim.Config, r sim.Result) error {
	s := r.Stats
	if s.Generated != s.Delivered+s.Expired+s.Rejected+s.Pending || s.Pending < 0 || s.SourceBytes < 0 || s.SourceBytes > c.SourceCapacityBytes {
		return fmt.Errorf("invalid accounting")
	}
	limit := c.Budget
	if r.Policy == selector.All {
		limit = len(c.Relays)
	}
	delivered, rejected, expired, assigned := 0, 0, 0, 0
	for _, m := range r.Messages {
		if len(m.Recipients) > limit {
			return fmt.Errorf("copy budget exceeded")
		}
		seen := map[string]bool{}
		for _, id := range m.Recipients {
			if seen[id] {
				return fmt.Errorf("duplicate recipient")
			}
			seen[id] = true
		}
		assigned += len(m.Recipients)
		if m.DeliveredAt >= 0 {
			delivered++
			if m.DeliveredAt < m.Created || m.DeliveredAt > m.Deadline {
				return fmt.Errorf("late delivery counted")
			}
		}
		if m.Rejected {
			rejected++
		}
		if m.Expired {
			expired++
		}
		if m.Rejected && (m.Expired || m.DeliveredAt >= 0) || m.Expired && m.DeliveredAt >= 0 {
			return fmt.Errorf("overlapping terminal states")
		}
	}
	if len(r.Messages) != s.Generated || delivered != s.Delivered || expired != s.Expired || rejected != s.Rejected || assigned != s.Assigned || s.MaxCopies > limit+1 {
		return fmt.Errorf("counters differ from message ledger")
	}
	return nil
}

func estimate(v []float64, seed int64) Estimate {
	n := float64(len(v))
	e := Estimate{}
	for _, x := range v {
		e.Mean += x / n
	}
	r := rand.New(rand.NewSource(seed))
	means := make([]float64, 2000)
	for b := range means {
		for range v {
			means[b] += v[r.Intn(len(v))] / n
		}
	}
	sort.Float64s(means)
	e.Low, e.High = means[49], means[1949]
	return e
}
func summarize(rows []Row, runs int, seed int64) ([]Summary, []Comparison) {
	var summaries []Summary
	var pairs []Comparison
	for _, c := range Cases() {
		by := map[selector.Policy][]Row{}
		for _, r := range rows {
			if r.Case == c.Name {
				by[r.Policy] = append(by[r.Policy], r)
			}
		}
		for _, p := range sim.ResearchPolicies() {
			s := Summary{Case: c.Name, Policy: p, Runs: runs}
			var rates, bytes []float64
			for _, r := range by[p] {
				rates = append(rates, r.Stats.DeliveryRate)
				bytes = append(bytes, float64(r.Stats.DataBytes+r.Stats.ControlBytes))
				if r.Stats.Delivered > 0 {
					s.P95Mean += float64(r.Stats.P95MS)
					s.LatencyRuns++
				}
				s.CopiesMean += float64(r.Stats.Assigned) / float64(r.Stats.Generated) / float64(runs)
			}
			if s.LatencyRuns > 0 {
				s.P95Mean /= float64(s.LatencyRuns)
			}
			s.Delivery = estimate(rates, seed)
			s.Bytes = estimate(bytes, seed)
			summaries = append(summaries, s)
		}
		for _, p := range []selector.Policy{selector.Availability, selector.Diverse, selector.RiskAware, selector.All, sim.AdaptiveIndependent, sim.AdaptiveNoDeadline, sim.AdaptiveStatic} {
			var delta, saving []float64
			allEqual := true
			for i, r := range by[p] {
				a := by[sim.Adaptive][i]
				delta = append(delta, a.Stats.DeliveryRate-r.Stats.DeliveryRate)
				allEqual = allEqual && a.Stats.DeliveryRate == r.Stats.DeliveryRate
				oldBytes := r.Stats.DataBytes + r.Stats.ControlBytes
				saving = append(saving, 1-float64(a.Stats.DataBytes+a.Stats.ControlBytes)/float64(oldBytes))
			}
			x := Comparison{Case: c.Name, Baseline: p, DeliveryDelta: estimate(delta, seed), ByteSaving: estimate(saving, seed), Decision: "Неопределённость: преимущество по доставке не установлено"}
			if x.DeliveryDelta.Low > 0 {
				x.Decision = "Положительная парная разность доставки в данной модели"
			} else if x.DeliveryDelta.High < 0 {
				x.Decision = "Доставка хуже контрольного метода в данной модели"
			} else if allEqual && x.ByteSaving.Low > 0 {
				x.Decision = "В серии доставка совпала; модельных байтов меньше"
			}
			pairs = append(pairs, x)
		}
	}
	return summaries, pairs
}

func Generate(ctx context.Context, dest string, runs int, seed int64, out io.Writer) error {
	if runs < 2 || runs > 100 {
		return fmt.Errorf("research requires 2..100 independent runs")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0755); err != nil {
		return fmt.Errorf("choose a new output directory: %w", err)
	}
	var rows []Row
	var worlds []sim.Config
	started := time.Now()
	for _, base := range Cases() {
		for run := 0; run < runs; run++ {
			c := Variant(base, seed+int64(run))
			worlds = append(worlds, c)
			for _, p := range sim.ResearchPolicies() {
				r, err := sim.Run(ctx, c, p, false)
				if err != nil {
					return fmt.Errorf("%s/%s: %w", c.Name, p, err)
				}
				if err := ValidateResult(c, r); err != nil {
					return err
				}
				rows = append(rows, Row{c.Name, run, c.Seed, p, r.Stats})
			}
		}
		fmt.Fprintf(out, "validated %-20s %d runs x %d policies\n", base.Name, runs, len(sim.ResearchPolicies()))
	}
	r := Report{Protocol: Protocol, Model: sim.Version, Algorithm: adaptive.Revision, Seed: seed, Runs: runs, Checks: len(rows)}
	r.Summary, r.Comparisons = summarize(rows, runs, seed^0x9e3779b9)
	writeJSON := func(name string, v any) error {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, name), append(data, '\n'), 0644)
	}
	if err := writeJSON("summary.json", r); err != nil {
		return err
	}
	if err := writeJSON("worlds.json", worlds); err != nil {
		return err
	}
	data, _ := json.Marshal(worlds)
	meta := map[string]any{"protocol": Protocol, "algorithm": adaptive.Revision, "model": sim.Version, "worlds_canonical_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "go": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "runs": runs, "base_seed": seed, "checked_policy_runs": len(rows), "window": adaptive.Window, "prior_weight": adaptive.PriorWeight, "copy_cost": adaptive.CopyCost, "elapsed_seconds": time.Since(started).Seconds(), "uncertainty": "percentile block bootstrap across independent seeds; 2000 resamples; intervals are per comparison, not simultaneous"}
	if err := writeJSON("manifest.json", meta); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(dest, "runs.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(file)
	_ = w.Write([]string{"case", "run", "seed", "policy", "generated", "delivered", "expired", "rejected", "pending", "delivery_rate", "data_bytes", "control_bytes", "probe_bytes", "assigned", "p95_ms", "evaluated_sets"})
	for _, r := range rows {
		s := r.Stats
		_ = w.Write([]string{r.Case, strconv.Itoa(r.Run), strconv.FormatInt(r.Seed, 10), string(r.Policy), strconv.Itoa(s.Generated), strconv.Itoa(s.Delivered), strconv.Itoa(s.Expired), strconv.Itoa(s.Rejected), strconv.Itoa(s.Pending), fmt.Sprintf("%.9f", s.DeliveryRate), strconv.FormatInt(s.DataBytes, 10), strconv.FormatInt(s.ControlBytes, 10), strconv.FormatInt(s.ProbeBytes, 10), strconv.Itoa(s.Assigned), strconv.FormatInt(s.P95MS, 10), strconv.Itoa(s.Evaluated)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := WriteEvidence(dest, r); err != nil {
		return err
	}
	if err := dataArchive(dest); err != nil {
		return err
	}
	fmt.Fprintln(out, "Evidence:", filepath.Join(dest, "evidence.html"))
	return nil
}
