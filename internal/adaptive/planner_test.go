package adaptive

import (
	"reflect"
	"testing"
)

func fixture(t *testing.T) (*Learner, []Node, Options) {
	t.Helper()
	l, err := New([]Identity{{"A", .72}, {"B", .68}, {"C", .56}})
	if err != nil {
		t.Fatal(err)
	}
	n := []Node{{ID: "A", Groups: []string{"AP1"}, Known: true, Ready: true, FreeBytes: 4096, LocalMS: 40, ServiceMS: 80}, {ID: "B", Groups: []string{"AP1"}, Known: true, Ready: true, FreeBytes: 4096, LocalMS: 40, ServiceMS: 80}, {ID: "C", Groups: []string{"AP2"}, Known: true, Ready: true, FreeBytes: 4096, LocalMS: 40, ServiceMS: 80}}
	return l, n, Options{Budget: 2, Bytes: 512, RemainingMS: 12000}
}
func TestLearnsQualityChangeAndDeterministicSets(t *testing.T) {
	l, n, o := fixture(t)
	for i := 1; i <= 24; i++ {
		if err := l.Observe(int64(i), map[string]bool{"A": false, "B": false, "C": true}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := l.Choose(24, n, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Chosen) != 1 || p.Chosen[0] != "C" {
		t.Fatalf("did not learn C: %+v", p)
	}
	n[0], n[2] = n[2], n[0]
	again, err := l.Choose(24, n, nil, o)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("order-dependent result", err)
	}
	if _, err := l.Choose(23, n, nil, o); err == nil {
		t.Fatal("future knowledge accepted")
	}
}
func TestBudgetDeadlineAndZeroBudget(t *testing.T) {
	l, n, o := fixture(t)
	o.Budget = 0
	p, err := l.Choose(0, n, nil, o)
	if err != nil || len(p.Chosen) != 0 {
		t.Fatal("zero budget", err)
	}
	o.Budget = 2
	n[0].ServiceMS = 30000
	n[1].ServiceMS = 30000
	p, err = l.Choose(0, n, nil, o)
	if err != nil || len(p.Chosen) != 1 || p.Chosen[0] != "C" {
		t.Fatalf("deadline ignored: %+v %v", p, err)
	}
	n[0].ServiceMS = 80
	n[1].ServiceMS = 80
	o.Budget = 1
	p, err = l.Choose(0, n, []string{"A"}, o)
	if err != nil || len(p.Chosen) != 0 {
		t.Fatal("lifetime budget renewed", err)
	}
}
func TestJointHistoryMattersAndColdStartIsFinite(t *testing.T) {
	l, n, o := fixture(t)
	n[0].Groups = nil
	n[1].Groups = nil
	n[2].Groups = nil
	for i := 1; i <= 24; i++ {
		x := i%2 == 0
		_ = l.Observe(int64(i), map[string]bool{"A": x, "B": x, "C": !x})
	}
	p, err := l.Choose(24, n, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Chosen, []string{"A", "C"}) {
		t.Fatalf("lost complementary history: %+v", p)
	}
	o.Independent = true
	q, err := l.Choose(24, n, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q.Chosen, []string{"A", "B"}) {
		t.Fatalf("ablation does not isolate joint history: %+v", q)
	}
	if p.Expected < 0 || p.Expected > 1 || p.Evaluated < 1 {
		t.Fatal("invalid estimate")
	}
	if len(l.Pairs()) != 3 || l.Pairs()[0].Phi != 1 {
		t.Fatal("incorrect correlation diagnostics")
	}
}
func TestObservationValidation(t *testing.T) {
	if _, err := New([]Identity{{"x", 2}}); err == nil {
		t.Fatal("invalid prior")
	}
	l, _, _ := fixture(t)
	if err := l.Observe(1, map[string]bool{"A": true}); err == nil {
		t.Fatal("partial observation")
	}
	full := map[string]bool{"A": false, "B": false, "C": false}
	if err := l.Observe(1, full); err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(1, full); err == nil {
		t.Fatal("replayed observation")
	}
	if h := l.Health(); len(h) != 3 || h[0].Samples != 1 {
		t.Fatal("missing health samples")
	}
}
func BenchmarkChoose12(b *testing.B) {
	var ids []Identity
	var nodes []Node
	for i := 0; i < 12; i++ {
		id := string(rune('A' + i))
		ids = append(ids, Identity{id, .75})
		nodes = append(nodes, Node{ID: id, Known: true, Ready: true, FreeBytes: 4096, LocalMS: 20, ServiceMS: 50})
	}
	l, _ := New(ids)
	o := Options{Budget: 3, Bytes: 512, RemainingMS: 12000}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Choose(0, nodes, nil, o); err != nil {
			b.Fatal(err)
		}
	}
}
