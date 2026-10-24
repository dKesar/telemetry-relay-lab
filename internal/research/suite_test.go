package research

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

func TestVariantIsValidReproducibleAndDoesNotMutate(t *testing.T) {
	for _, c := range Cases() {
		before, _ := json.Marshal(c)
		for seed := int64(1000); seed < 1020; seed++ {
			a, b := Variant(c, seed), Variant(c, seed)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("nondeterministic world")
			}
			if err := a.Validate(); err != nil {
				t.Fatal(c.Name, err)
			}
		}
		after, _ := json.Marshal(c)
		if string(before) != string(after) {
			t.Fatal("mutated base case")
		}
	}
}
func TestEvidenceExportAndNoOverwrite(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "proof")
	if err := Generate(context.Background(), dest, 2, 1000, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.json", "worlds.json", "runs.csv", "manifest.json", "delivery.svg", "gain.svg", "cost.svg", "evidence.html"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || len(data) == 0 {
			t.Fatal(name, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dest, "summary.json"))
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Checks != len(Cases())*2*len(sim.ResearchPolicies()) || len(r.Summary) != len(Cases())*len(sim.ResearchPolicies()) {
		t.Fatal("incomplete report")
	}
	for _, s := range r.Summary {
		if s.Delivery.Mean < s.Delivery.Low-1e-9 || s.Delivery.Mean > s.Delivery.High+1e-9 {
			t.Fatal("bad interval")
		}
	}
	if err := Rebuild(dest); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dest, "summary.json"))
	if string(data) != string(after) {
		t.Fatal("rebuild changed a numerical result")
	}
	if err := Generate(context.Background(), dest, 2, 1000, os.Stdout); err == nil {
		t.Fatal("overwrote evidence")
	}
	if err := Generate(context.Background(), filepath.Join(t.TempDir(), "bad"), 1, 1000, os.Stdout); err == nil {
		t.Fatal("accepted singleton CI")
	}
}
func TestValidatorRejectsBudgetAndLateDelivery(t *testing.T) {
	c, _ := sim.Preset("shared_failure")
	r, err := sim.Run(context.Background(), c, sim.Adaptive, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(c, r); err != nil {
		t.Fatal(err)
	}
	r.Messages[0].Recipients = append(r.Messages[0].Recipients, "extra", "extra2")
	if err := ValidateResult(c, r); err == nil {
		t.Fatal("bad ledger accepted")
	}
	r.Messages[0].Recipients = nil
	r.Messages[0].DeliveredAt = r.Messages[0].Deadline + 1
	if err := ValidateResult(c, r); err == nil || !strings.Contains(err.Error(), "late") {
		t.Fatal("late delivery accepted", err)
	}
}
