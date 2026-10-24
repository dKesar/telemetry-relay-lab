package sim

import (
	"context"
	"reflect"
	"testing"
)

func TestAdaptiveAllPresetsAndTraceIndependence(t *testing.T) {
	for _, c := range Presets() {
		for _, p := range []string{"adaptive", "adaptive_independent", "adaptive_no_deadline", "adaptive_static"} {
			policy := Adaptive
			if p == "adaptive_independent" {
				policy = AdaptiveIndependent
			}
			if p == "adaptive_no_deadline" {
				policy = AdaptiveNoDeadline
			}
			if p == "adaptive_static" {
				policy = AdaptiveStatic
			}
			r, err := Run(context.Background(), c, policy, true)
			if err != nil {
				t.Fatalf("%s/%s: %v", c.Name, p, err)
			}
			assertInvariants(t, c, r)
			n, err := Run(context.Background(), c, policy, false)
			if err != nil || !reflect.DeepEqual(r.Stats, n.Stats) {
				t.Fatal("trace influenced adaptive algorithm", err)
			}
			if r.Stats.ProbeBytes == 0 || r.Stats.Decisions == 0 {
				t.Fatal("learning not exercised")
			}
		}
	}
}
func TestFutureFailurePlanDoesNotLeak(t *testing.T) {
	c, _ := Preset("changing_links")
	a, err := Run(context.Background(), c, Adaptive, true)
	if err != nil {
		t.Fatal(err)
	}
	c.Relays[0].Phases = []LinkPhase{{30000, 60000, 1}}
	c.Relays[1].Phases = nil
	b, err := Run(context.Background(), c, Adaptive, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range a.Frames {
		if f.Time >= 10000 {
			break
		}
		if !reflect.DeepEqual(f, b.Frames[i]) {
			t.Fatalf("future leaked before change at %d", f.Time)
		}
	}
}
