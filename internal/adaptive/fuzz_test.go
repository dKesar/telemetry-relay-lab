package adaptive

import (
	"math"
	"reflect"
	"testing"
)

func FuzzBudgetAndDeterminism(f *testing.F) {
	f.Add([]byte{3, 2, 100, 12, 48, 70, 99, 8})
	f.Add([]byte{6, 0, 255, 0, 0, 255, 18, 32})
	f.Add([]byte{1, 1, 0, 255, 255, 0, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}
		n := 1 + int(data[0]%6)
		budget := int(data[1]) % (n + 1)
		var ids []Identity
		var nodes []Node
		for i := 0; i < n; i++ {
			id := string(rune('A' + i))
			ids = append(ids, Identity{id, float64(data[(i+2)%len(data)]) / 255})
			nodes = append(nodes, Node{ID: id, Known: data[3]%2 == 0, Ready: true, Groups: []string{string(rune('X' + i%2))}, Queue: float64(data[(i+4)%len(data)]) / 255, FreeBytes: 4096, LocalMS: float64(data[4]), ServiceMS: float64(data[5]) * 5})
		}
		l, err := New(ids)
		if err != nil {
			t.Fatal(err)
		}
		o := Options{Budget: budget, Bytes: 512, RemainingMS: 500 + float64(data[6])*10, SourceQueue: float64(data[7]) / 255}
		for at := 1; at <= 5; at++ {
			up := map[string]bool{}
			for i, id := range ids {
				up[id.ID] = data[(i+at)%len(data)]%3 != 0
			}
			if err := l.Observe(int64(at), up); err != nil {
				t.Fatal(err)
			}
		}
		p, err := l.Choose(5, nodes, nil, o)
		if err != nil {
			t.Fatal(err)
		}
		q, err := l.Choose(5, nodes, nil, o)
		if err != nil || !reflect.DeepEqual(p, q) {
			t.Fatal("not deterministic")
		}
		if len(p.Chosen) > budget || p.Expected < 0 || p.Expected > 1 || math.IsNaN(p.Utility) || math.IsInf(p.Utility, 0) || p.Evaluated > 1<<n {
			t.Fatalf("invalid bounded plan: %+v", p)
		}
		seen := map[string]bool{}
		for _, id := range p.Chosen {
			if seen[id] {
				t.Fatal("duplicate replica")
			}
			seen[id] = true
		}
	})
}
