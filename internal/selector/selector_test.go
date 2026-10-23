package selector

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func fixture() ([]Candidate, Options) {
	cs := []Candidate{
		{ID: "A", Availability: 0.72, FreeBytes: 1000, RiskKnown: true, Risks: []string{"AP1"}},
		{ID: "B", Availability: 0.68, FreeBytes: 1000, RiskKnown: true, Risks: []string{"AP1"}},
		{ID: "C", Availability: 0.56, FreeBytes: 1000, RiskKnown: true, Risks: []string{"AP2"}},
	}
	return cs, Options{Budget: 2, MessageBytes: 512, DeadlineMillis: 15000, Lambda: 1, UnknownPenalty: 0.5}
}

func ids(ds []Decision) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID
	}
	return out
}

func TestSelectionTradeoff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		c      float64
		policy Policy
		want   []string
	}{
		{"individual", .56, Availability, []string{"A", "B"}},
		{"diversity", .56, Diverse, []string{"A", "C"}},
		{"risk", .56, RiskAware, []string{"A", "C"}},
		{"weak", .08, RiskAware, []string{"A", "B"}},
		{"hard_diversity_weak", .08, Diverse, []string{"A", "C"}},
		{"all", .56, All, []string{"A", "B", "C"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, opts := fixture()
			cs[2].Availability = tc.c
			d, err := Select(cs, opts, tc.policy, nil)
			if err != nil || !reflect.DeepEqual(ids(d), tc.want) {
				t.Fatalf("got %v (%v); want %v", d, err, tc.want)
			}
		})
	}
}

func TestUnknownAndOverlappingRisk(t *testing.T) {
	cs, _ := fixture()
	tests := []struct {
		name      string
		candidate Candidate
		selected  []Candidate
		want      float64
	}{
		{"first unknown", Candidate{RiskKnown: false}, nil, 0},
		{"unknown is not independent", Candidate{RiskKnown: false}, cs[:1], .5},
		{"known empty", Candidate{RiskKnown: true}, cs[:1], 0},
		{"observed overlap wins", Candidate{RiskKnown: false, Risks: []string{"AP1"}}, cs[:1], 1},
		{"previous unknown", cs[2], []Candidate{{RiskKnown: false}}, .5},
	}
	for _, tc := range tests {
		if got := risk(tc.candidate, tc.selected, .5); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestEligibilityBoundaries(t *testing.T) {
	cs, opts := fixture()
	cs[0].FreeBytes = 511
	cs[1].TransferMillis = 15000.01
	cs[2].ReportAgeMillis = 60000
	d, err := Select(cs, opts, RiskAware, nil)
	if err != nil || !reflect.DeepEqual(ids(d), []string{"C"}) {
		t.Fatalf("boundary selection: %v %v", d, err)
	}
	cs[2].ReportAgeMillis = 60000.01
	d, err = Select(cs, opts, RiskAware, nil)
	if err != nil || len(d) != 0 {
		t.Fatalf("stale node selected: %v %v", d, err)
	}
}

func TestHistoryAndFreshness(t *testing.T) {
	for _, tc := range []struct {
		s, n int
		want float64
	}{{0, 0, .5}, {71, 98, .72}, {67, 98, .68}, {55, 98, .56}} {
		got, err := HistoricalScore(tc.s, tc.n)
		if err != nil || math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("history: %v %v", got, err)
		}
	}
	if _, err := HistoricalScore(2, 1); err == nil {
		t.Fatal("accepted impossible history")
	}
	c := Candidate{Availability: .8, QueueFraction: 1, ReportAgeMillis: 30000}
	if math.Abs(baseScore(c)-.2) > 1e-12 {
		t.Fatal("expected queue penalty and age half-life")
	}
}

func TestDeterminismAndInputNotMutated(t *testing.T) {
	cs, opts := fixture()
	cs[0].Availability = .5
	cs[1].Availability = .5
	cs[2].Availability = .5
	cs[0], cs[2] = cs[2], cs[0]
	before := append([]Candidate(nil), cs...)
	d, err := Select(cs, opts, Availability, nil)
	if err != nil || !reflect.DeepEqual(ids(d), []string{"A", "B"}) {
		t.Fatalf("tie-breaking: %v %v", d, err)
	}
	if !reflect.DeepEqual(cs, before) {
		t.Fatal("caller slice mutated")
	}
	a, _ := Select(cs, opts, Random, rand.New(rand.NewSource(42)))
	b, _ := Select(cs, opts, Random, rand.New(rand.NewSource(42)))
	if !reflect.DeepEqual(a, b) {
		t.Fatal("random selection not reproducible")
	}
	opts.Budget = 0
	d, _ = Select(cs, opts, RiskAware, nil)
	if len(d) != 0 {
		t.Fatal("zero budget allocated a copy")
	}
	opts.Budget = 100
	d, _ = Select(cs, opts, RiskAware, nil)
	if len(d) != 3 {
		t.Fatal("budget must be capped at candidate count")
	}
}

func TestInvalidInput(t *testing.T) {
	for _, mutate := range []func([]Candidate, *Options){
		func(c []Candidate, o *Options) { c[1].ID = c[0].ID },
		func(c []Candidate, o *Options) { c[0].Availability = math.NaN() },
		func(c []Candidate, o *Options) { c[0].QueueFraction = 1.01 },
		func(c []Candidate, o *Options) { c[0].ReportAgeMillis = -1 },
		func(c []Candidate, o *Options) { c[0].FreeBytes = -1 },
		func(c []Candidate, o *Options) { c[0].Risks = []string{"", "AP1"} },
		func(c []Candidate, o *Options) { o.Budget = -1 },
		func(c []Candidate, o *Options) { o.Lambda = math.Inf(1) },
		func(c []Candidate, o *Options) { o.UnknownPenalty = 1.1 },
	} {
		cs, o := fixture()
		mutate(cs, &o)
		if _, err := Select(cs, o, RiskAware, nil); err == nil {
			t.Fatal("accepted invalid selector input")
		}
	}
	cs, o := fixture()
	if _, err := Select(cs, o, Random, nil); err == nil {
		t.Fatal("accepted nil RNG")
	}
	if _, err := Select(cs, o, "bogus", nil); err == nil {
		t.Fatal("accepted unknown policy")
	}
}

func BenchmarkSelect(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("nodes_%d", n), func(b *testing.B) {
			cs := make([]Candidate, n)
			for i := range cs {
				cs[i] = Candidate{ID: fmt.Sprintf("node-%04d", i), Availability: float64(i%90+10) / 100, FreeBytes: 10000, RiskKnown: true, Risks: []string{fmt.Sprintf("ap-%d", i%12)}}
			}
			o := Options{Budget: 4, MessageBytes: 512, DeadlineMillis: 15000, Lambda: 1, UnknownPenalty: .5}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Select(cs, o, RiskAware, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
