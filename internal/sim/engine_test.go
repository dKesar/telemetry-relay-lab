package sim

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

func assertInvariants(t *testing.T, c Config, r Result) {
	t.Helper()
	if r.Stats.Generated != r.Stats.Delivered+r.Stats.Expired+r.Stats.Rejected+r.Stats.Pending {
		t.Fatal("message accounting does not balance")
	}
	limit := c.Budget
	if r.Policy == selector.All {
		limit = len(c.Relays)
	}
	for _, m := range r.Messages {
		if len(m.Recipients) > limit {
			t.Fatalf("budget exceeded: %+v", m)
		}
		ids := map[string]bool{}
		for _, id := range m.Recipients {
			if ids[id] {
				t.Fatal("duplicate ledger entry")
			}
			ids[id] = true
		}
		if m.DeliveredAt >= 0 && (m.DeliveredAt < m.Created || m.DeliveredAt > m.Deadline) {
			t.Fatal("delivery outside deadline")
		}
		if m.Rejected && (m.DeliveredAt >= 0 || m.Expired) {
			t.Fatal("rejected message delivered or expired twice")
		}
	}
	var generated, delivered, expired int
	for _, f := range r.Frames {
		if f.Stats.Generated < generated || f.Stats.Delivered < delivered || f.Stats.Expired < expired {
			t.Fatal("nonmonotonic counters")
		}
		generated, delivered, expired = f.Stats.Generated, f.Stats.Delivered, f.Stats.Expired
		if f.Stats.Generated != f.Stats.Delivered+f.Stats.Expired+f.Stats.Rejected+f.Stats.Pending {
			t.Fatal("frame accounting mismatch")
		}
		for _, n := range f.Nodes {
			if n.UsedBytes < 0 || n.UsedBytes > n.CapacityBytes {
				t.Fatalf("queue out of bounds: %+v", n)
			}
			if n.ID != "source" && n.ID != "collector" && n.UsedBytes != int64(n.Stored+n.Reserved)*c.MessageBytes {
				t.Fatalf("reservation accounting: %+v", n)
			}
		}
	}
	if len(r.Frames) > 0 && !reflect.DeepEqual(r.Frames[len(r.Frames)-1].Stats, r.Stats) {
		t.Fatal("final frame differs from final result")
	}
}

func TestAllPresets(t *testing.T) {
	for _, c := range Presets() {
		for _, p := range selector.Policies() {
			t.Run(c.Name+"/"+string(p), func(t *testing.T) {
				r, err := Run(context.Background(), c, p, true)
				if err != nil {
					t.Fatal(err)
				}
				assertInvariants(t, c, r)
				if r.Stats.Pending != 0 {
					t.Fatal("preset should drain until every deadline")
				}
				if r.Stats.MaxCopies > c.Budget+1 && p != selector.All {
					t.Fatal("source plus copies exceeded")
				}
			})
		}
	}
}

func simple() Config {
	c, _ := Preset("independent")
	c.DurationMS = 8000
	c.GenerateUntilMS = 3000
	c.TTLMS = 4000
	c.CollectorOutages = nil
	c.SourceDropProbability = 0
	c.AckLossProbability = 0
	for i := range c.Relays {
		c.Relays[i].DropProbability = 0
	}
	return c
}

func TestDeliveryAndConservation(t *testing.T) {
	c := simple()
	r, err := Run(context.Background(), c, selector.RiskAware, true)
	if err != nil {
		t.Fatal(err)
	}
	assertInvariants(t, c, r)
	if r.Stats.Generated != r.Stats.Delivered || r.Stats.SourceBytes != 0 {
		t.Fatalf("perfect links did not drain: %+v", r.Stats)
	}
	c.Budget = 0
	r, err = Run(context.Background(), c, selector.RiskAware, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.Delivered != 0 || r.Stats.Expired != r.Stats.Generated {
		t.Fatal("zero budget created delivery")
	}
	c.SourceCapacityBytes = 0
	r, err = Run(context.Background(), c, selector.RiskAware, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.Rejected != r.Stats.Generated {
		t.Fatal("source capacity not enforced")
	}
}

func TestLostAckDoesNotRenewBudget(t *testing.T) {
	c := simple()
	c.AckLossProbability = 1
	r, err := Run(context.Background(), c, selector.RiskAware, true)
	if err != nil {
		t.Fatal(err)
	}
	assertInvariants(t, c, r)
	if r.Stats.Delivered == 0 || r.Stats.Retries == 0 || r.Stats.Duplicates == 0 || r.Stats.LocalDuplicates == 0 {
		t.Fatalf("lost ACK not exercised: %+v", r.Stats)
	}
}

func TestFailureDuringTransfer(t *testing.T) {
	c := simple()
	c.GenerateUntilMS = 1
	c.Budget = 1
	c.Domains[0].Outages = []Interval{{10, 7000}}
	c.Relays = c.Relays[:2]
	r, err := Run(context.Background(), c, selector.Availability, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.Delivered != 0 {
		t.Fatal("transfer survived an outage inside its interval")
	}
	for _, e := range r.Events {
		if e.Kind == "stored" {
			t.Fatal("partially transferred message became durable")
		}
	}
}

func TestDeterminismAndTraceIndependence(t *testing.T) {
	c := simple()
	a, err := Run(context.Background(), c, selector.Random, true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(context.Background(), c, selector.Random, true)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) != string(y) {
		t.Fatal("same seed changed replay")
	}
	n, err := Run(context.Background(), c, selector.Random, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Stats, n.Stats) {
		t.Fatal("trace altered simulation")
	}
	if draw(42, 1, "A", "uplink_data", 1) != draw(42, 1, "A", "uplink_data", 1) {
		t.Fatal("unstable counter RNG")
	}
}

func TestBadConfigAndCancellation(t *testing.T) {
	mutations := []func(*Config){func(c *Config) { c.Budget = 99 }, func(c *Config) { c.IntervalMS = 0 }, func(c *Config) { c.Relays[0].ID = "source" }, func(c *Config) { c.Relays[0].PhysicalGroups = []string{"missing"} }, func(c *Config) { c.AckLossProbability = 1.1 }, func(c *Config) { c.Domains[0].Outages = []Interval{{100, 50}} }}
	for _, f := range mutations {
		c := simple()
		f(&c)
		if _, err := Run(context.Background(), c, selector.RiskAware, false); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, simple(), selector.RiskAware, false); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := Preset("absent"); err == nil {
		t.Fatal("accepted unknown preset")
	}
}

func TestIntervalBoundaries(t *testing.T) {
	xs := []Interval{{10, 20}}
	if blocked(xs, 0, 9) || !blocked(xs, 0, 10) || blocked(xs, 20, 21) {
		t.Fatal("half-open outage boundary regression")
	}
}
