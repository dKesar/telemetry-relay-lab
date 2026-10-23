package experiment

import (
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/dKesar/telemetry-relay-lab/internal/model"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

func paper(t *testing.T) model.Suite {
	t.Helper()
	s, _, err := model.Load("../../configs/paper.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPaperExact(t *testing.T) {
	wants := map[string]map[selector.Policy]string{
		"shared_failure":       {selector.Availability: "0.788", selector.Diverse: "0.8768", selector.RiskAware: "0.8768", selector.All: "0.90672", selector.Random: "631/750"},
		"independent_failures": {selector.Availability: "0.9104", selector.Diverse: "0.8768", selector.RiskAware: "0.8768", selector.All: "0.960576", selector.Random: "1654/1875"},
		"weak_alternative":     {selector.Availability: "0.788", selector.Diverse: "0.7424", selector.RiskAware: "0.788", selector.All: "0.80496", selector.Random: "559/750"},
		"wrong_labels":         {selector.Availability: "0.788", selector.Diverse: "0.788", selector.RiskAware: "0.788", selector.All: "0.90672", selector.Random: "631/750"},
	}
	for _, s := range paper(t).Scenarios {
		rows, err := Exact(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			want, ok := new(big.Rat).SetString(wants[s.Name][r.Policy])
			if !ok {
				t.Fatal("bad expectation")
			}
			if r.Probability.Cmp(want) != 0 {
				t.Errorf("%s/%s: got %s want %s", s.Name, r.Policy, r.Probability.RatString(), want.RatString())
			}
			if r.Policy != selector.All && r.Copies > s.Options.Budget {
				t.Fatal("copy budget exceeded")
			}
		}
	}
}

func TestZeroLambdaMatchesBaseline(t *testing.T) {
	for _, s := range paper(t).Scenarios {
		s.Options.Lambda = 0
		a, err := selector.Select(s.Candidates(), s.Options, selector.Availability, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, err := selector.Select(s.Candidates(), s.Options, selector.RiskAware, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := range a {
			if a[i].ID != b[i].ID {
				t.Fatal("lambda=0 differs from baseline")
			}
		}
	}
}

func TestMonteCarloAgainstExactAndReproducibility(t *testing.T) {
	const trials = 30000
	for _, s := range paper(t).Scenarios {
		got, err := Run(s, 42, trials)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Run(s, 42, trials)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatal("same seed changed results")
		}
		exact, err := Exact(s)
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range got {
			p, _ := exact[i].Probability.Float64()
			tolerance := 6*math.Sqrt(p*(1-p)/trials) + 1.0/trials
			if math.Abs(r.Rate()-p) > tolerance {
				t.Fatalf("%s/%s: Monte Carlo=%v exact=%v", s.Name, r.Policy, r.Rate(), p)
			}
			if r.Policy != selector.All && r.Copies > 2 {
				t.Fatal("copy budget exceeded")
			}
		}
	}
}

func TestZeroBudgetAndNoEligibleNodes(t *testing.T) {
	s := paper(t).Scenarios[0]
	s.Options.Budget = 0
	rows, err := Exact(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Policy != selector.All && r.Probability.Sign() != 0 {
			t.Fatal("zero-budget delivery")
		}
	}
	for i := range s.Relays {
		s.Relays[i].Observed.FreeBytes = 0
	}
	rows, err = Exact(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Probability.Sign() != 0 {
			t.Fatal("ineligible delivery")
		}
	}
	if _, err := Run(s, 1, 0); err == nil {
		t.Fatal("accepted zero trials")
	}
}

func TestSummaryIsPairedAndSeeded(t *testing.T) {
	s := paper(t).Scenarios[0]
	var runs [][]RunResult
	for seed := int64(1); seed <= 5; seed++ {
		r, err := Run(s, seed, 1000)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	a, err := Summarize(runs, 42)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Summarize(runs, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("bootstrap not reproducible")
	}
	for _, r := range a {
		if r.Policy == selector.Availability && (r.DeltaMean != 0 || r.DeltaLower != 0 || r.DeltaUpper != 0) {
			t.Fatal("baseline paired delta is not zero")
		}
	}
	if _, err := Summarize(nil, 42); err == nil {
		t.Fatal("accepted empty runs")
	}
	if _, err := Summarize([][]RunResult{{{Policy: selector.Random, Trials: 1}}}, 42); err == nil {
		t.Fatal("accepted incomplete run")
	}
}
