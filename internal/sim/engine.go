package sim

import (
	"container/heap"
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

type assignment struct {
	allocated, stored, localAcked, localBusy, upBusy bool
	localAttempts, upAttempts                        int
	nextLocal, nextUp                                int64
}
type messageState struct {
	Message
	source, done bool
	assigned     []*assignment
}
type relayState struct {
	used int64
	busy bool
}

// Completions precede new work and frames at the same simulated instant.
const (
	localData = iota
	uplinkData
	localAck
	serverAck
	sourceReceipt
	expire
	create
	tick
	frame
)

type pending struct {
	at                   int64
	seq                  int
	kind, relay, attempt int
	m                    *messageState
	start                int64
}
type agenda []pending

func (a agenda) Len() int { return len(a) }
func (a agenda) Less(i, j int) bool {
	if a[i].at != a[j].at {
		return a[i].at < a[j].at
	}
	if a[i].kind != a[j].kind {
		return a[i].kind < a[j].kind
	}
	return a[i].seq < a[j].seq
}
func (a agenda) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a *agenda) Push(x any)   { *a = append(*a, x.(pending)) }
func (a *agenda) Pop() any     { old := *a; x := old[len(old)-1]; *a = old[:len(old)-1]; return x }

type engine struct {
	c            Config
	policy       selector.Policy
	trace        bool
	q            agenda
	seq          int
	messages     []*messageState
	relays       []relayState
	stats        Stats
	sourceActive int
	flights      map[int]Flight
	result       Result
}

func Run(ctx context.Context, c Config, policy selector.Policy, trace bool) (Result, error) {
	if err := c.Validate(); err != nil {
		return Result{}, err
	}
	valid := false
	for _, p := range selector.Policies() {
		valid = valid || p == policy
	}
	if !valid {
		return Result{}, fmt.Errorf("unknown policy %q", policy)
	}
	e := engine{c: c, policy: policy, trace: trace, relays: make([]relayState, len(c.Relays)), flights: make(map[int]Flight), result: Result{Policy: policy, Frames: []Frame{}, Events: []Event{}, Messages: []Message{}}}
	for t := int64(0); t < c.GenerateUntilMS; t += c.IntervalMS {
		e.push(pending{at: t, kind: create})
	}
	for t := int64(0); t <= c.DurationMS; t += TickMS {
		e.push(pending{at: t, kind: tick})
	}
	if trace {
		for t := int64(0); t < c.DurationMS; t += FrameMS {
			e.push(pending{at: t, kind: frame})
		}
		e.push(pending{at: c.DurationMS, kind: frame})
	}
	for e.q.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		x := heap.Pop(&e.q).(pending)
		e.handle(x)
		if x.kind != frame {
			if err := e.plan(x.at); err != nil {
				return Result{}, err
			}
		}
		if len(e.result.Events) > 100000 {
			return Result{}, fmt.Errorf("trace event limit exceeded; lower the workload")
		}
	}
	e.result.Stats = e.snapshotStats()
	for _, m := range e.messages {
		e.result.Messages = append(e.result.Messages, m.Message)
	}
	return e.result, nil
}

func RunAll(ctx context.Context, c Config, trace bool) (Report, error) {
	out := Report{Version: Version, Config: c, Results: []Result{}}
	for _, p := range selector.Policies() {
		r, err := Run(ctx, c, p, trace)
		if err != nil {
			return Report{}, err
		}
		out.Results = append(out.Results, r)
	}
	return out, nil
}

func (e *engine) push(x pending) {
	if x.at > e.c.DurationMS {
		return
	}
	e.seq++
	x.seq = e.seq
	heap.Push(&e.q, x)
}
func (e *engine) log(t int64, kind string, m *messageState, node, detail string) {
	if !e.trace {
		return
	}
	id := -1
	if m != nil {
		id = m.ID
	}
	e.result.Events = append(e.result.Events, Event{Time: t, Kind: kind, Message: id, Node: node, Detail: detail})
}
func blocked(xs []Interval, start, end int64) bool {
	for _, x := range xs {
		if start < x.End && end >= x.Start {
			return true
		}
	}
	return false
}
func (e *engine) localUp(i int, start, end int64) bool {
	for _, g := range e.c.Relays[i].PhysicalGroups {
		for _, d := range e.c.Domains {
			if d.ID == g && blocked(d.Outages, start, end) {
				return false
			}
		}
	}
	return true
}
func (e *engine) upstreamUp(i int, start, end int64) bool {
	return e.localUp(i, start, end) && !blocked(e.c.CollectorOutages, start, end)
}
func duration(bytes, rate, latency int64) int64 { return latency + (bytes*1000+rate-1)/rate }

// Counter-based common randomness: physical outcomes do not depend on how many
// RNG calls a policy made. A packet is keyed by seed, relay ID, phase, message,
// attempt. Scheduling and queue histories can still differ between policies.
func draw(seed int64, id int, relay, phase string, attempt int) float64 {
	h := uint64(14695981039346656037) ^ uint64(seed)
	for _, s := range []string{relay, phase} {
		for i := 0; i < len(s); i++ {
			h = (h ^ uint64(s[i])) * 1099511628211
		}
		h = (h ^ 255) * 1099511628211
	}
	h ^= uint64(id+1) * 0x9e3779b97f4a7c15
	h ^= uint64(attempt+1) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11) * (1.0 / (1 << 53))
}
func (e *engine) retry(m *messageState, i int, phase string, attempt int) int64 {
	base := int64(500) << min(attempt-1, 4)
	return int64(float64(base) * (.8 + .4*draw(e.c.Seed, m.ID, e.c.Relays[i].ID, phase, attempt)))
}
func (e *engine) release(m *messageState, i int) {
	a := m.assigned[i]
	if a == nil {
		return
	}
	if a.allocated {
		e.relays[i].used -= e.c.MessageBytes
		a.allocated = false
	}
	a.stored = false
}
func (e *engine) releaseSource(m *messageState) {
	if m.source {
		m.source = false
		e.stats.SourceBytes -= e.c.MessageBytes
	}
}

func (e *engine) handle(x pending) {
	t := x.at
	m := x.m
	i := x.relay
	if x.kind == create {
		m = &messageState{Message: Message{ID: len(e.messages), Created: t, Deadline: t + e.c.TTLMS, DeliveredAt: -1, Recipients: []string{}}, assigned: make([]*assignment, len(e.relays))}
		e.messages = append(e.messages, m)
		e.stats.Generated++
		if e.stats.SourceBytes+e.c.MessageBytes > e.c.SourceCapacityBytes {
			m.Rejected = true
			m.done = true
			e.stats.Rejected++
			e.log(t, "rejected", m, "source", "Нет места в очереди источника")
			return
		}
		m.source = true
		e.stats.SourceBytes += e.c.MessageBytes
		e.stats.MaxCopies = max(e.stats.MaxCopies, 1)
		e.log(t, "created", m, "source", "Создано сообщение")
		e.push(pending{at: m.Deadline + 1, kind: expire, m: m})
		return
	}
	if x.kind == frame {
		e.recordFrame(t)
		return
	}
	if x.kind == tick {
		return
	}
	if x.kind == expire {
		if m.done {
			return
		}
		m.done = true
		e.releaseSource(m)
		for j := range e.relays {
			e.release(m, j)
		}
		if m.DeliveredAt < 0 {
			m.Expired = true
			e.stats.Expired++
			e.log(t, "expired", m, "source", "Срок доставки истёк")
		}
		return
	}
	a := m.assigned[i]
	r := e.c.Relays[i]
	if x.kind == localData {
		a.localBusy = false
		e.sourceActive--
		delete(e.flights, x.seq)
		a.nextLocal = t + e.retry(m, i, "local_backoff", x.attempt)
		if m.done {
			return
		}
		if !e.localUp(i, x.start, t) || draw(e.c.Seed, m.ID, r.ID, "local_data", x.attempt) < e.c.SourceDropProbability {
			e.log(t, "lost", m, r.ID, "Локальная передача не завершена")
			return
		}
		if !a.allocated {
			if e.relays[i].used+e.c.MessageBytes > r.CapacityBytes {
				e.log(t, "lost", m, r.ID, "Очередь получателя заполнена")
				return
			}
			e.relays[i].used += e.c.MessageBytes
			a.allocated = true
		}
		if a.stored {
			e.stats.LocalDuplicates++
		}
		a.stored = true
		e.log(t, "stored", m, r.ID, "Копия сохранена в очереди ретранслятора")
		e.stats.ControlBytes += AckBytes
		e.push(pending{at: t + e.c.AckDelayMS, kind: localAck, m: m, relay: i, start: t, attempt: x.attempt})
		return
	}
	if x.kind == uplinkData {
		e.relays[i].busy = false
		a.upBusy = false
		delete(e.flights, x.seq)
		a.nextUp = t + e.retry(m, i, "uplink_backoff", x.attempt)
		if m.done {
			return
		}
		if !e.upstreamUp(i, x.start, t) || draw(e.c.Seed, m.ID, r.ID, "uplink_data", x.attempt) < r.DropProbability {
			e.log(t, "lost", m, r.ID, "Передача серверу потеряна")
			return
		}
		if m.DeliveredAt < 0 {
			m.DeliveredAt = t
			e.stats.Delivered++
			e.log(t, "delivered", m, r.ID, "Сервер сохранил сообщение до срока")
		} else {
			e.stats.Duplicates++
			e.log(t, "duplicate", m, r.ID, "Повтор принят без второй записи")
		}
		e.stats.ControlBytes += AckBytes
		e.push(pending{at: t + e.c.AckDelayMS, kind: serverAck, m: m, relay: i, start: t, attempt: x.attempt})
		return
	}
	if m.done {
		return
	}
	if x.kind == localAck {
		if e.localUp(i, x.start, t) && draw(e.c.Seed, m.ID, r.ID, "local_ack", x.attempt) >= e.c.AckLossProbability {
			a.localAcked = true
			e.log(t, "ack", m, r.ID, "Источник получил квитанцию размещения")
		} else {
			e.log(t, "ack_lost", m, r.ID, "Потеряна квитанция размещения; получатель остаётся прежним")
		}
		return
	}
	if x.kind == serverAck {
		if !e.upstreamUp(i, x.start, t) || draw(e.c.Seed, m.ID, r.ID, "server_ack", x.attempt) < e.c.AckLossProbability {
			e.log(t, "ack_lost", m, r.ID, "Потеряна серверная квитанция; копия остаётся в очереди")
			return
		}
		e.release(m, i)
		e.log(t, "ack", m, r.ID, "Серверная квитанция разрешила удалить копию")
		if m.source && e.localUp(i, t, t) {
			e.stats.ControlBytes += AckBytes
			e.push(pending{at: t + e.c.AckDelayMS, kind: sourceReceipt, m: m, relay: i, start: t, attempt: x.attempt})
		}
		return
	}
	if x.kind == sourceReceipt {
		if e.localUp(i, x.start, t) && draw(e.c.Seed, m.ID, r.ID, "source_receipt", x.attempt) >= e.c.AckLossProbability {
			e.releaseSource(m)
			e.log(t, "receipt", m, "source", "Источник получил серверную квитанцию")
		}
	}
}

func (e *engine) candidate(i int) selector.Candidate {
	r := e.c.Relays[i]
	q := 0.0
	if r.CapacityBytes > 0 {
		q = float64(e.relays[i].used) / float64(r.CapacityBytes)
	}
	return selector.Candidate{ID: r.ID, Availability: r.Availability, QueueFraction: q, FreeBytes: r.CapacityBytes - e.relays[i].used, TransferMillis: float64(duration(e.c.MessageBytes, r.LocalBytesPerSec, r.LatencyMS)), RiskKnown: r.GroupsKnown, Risks: r.ReportedGroups}
}

func (e *engine) plan(t int64) error {
	// Messages are created in order and have the same TTL: creation order is EDF.
	for _, m := range e.messages {
		if m.done || !m.source || t > m.Deadline {
			continue
		}
		var candidates, existing []selector.Candidate
		indices := make(map[string]int)
		for i, r := range e.c.Relays {
			indices[r.ID] = i
			if m.assigned[i] != nil {
				existing = append(existing, e.candidate(i))
			} else if e.localUp(i, t, t) {
				candidates = append(candidates, e.candidate(i))
			}
		}
		remaining := m.Deadline - t
		if remaining > 0 && len(candidates) > 0 && (e.policy == selector.All || len(existing) < e.c.Budget) {
			o := selector.Options{Budget: e.c.Budget, MessageBytes: e.c.MessageBytes, DeadlineMillis: float64(remaining), Lambda: e.c.Lambda, UnknownPenalty: e.c.UnknownPenalty}
			var rng *rand.Rand
			if e.policy == selector.Random {
				rng = rand.New(rand.NewSource(e.c.Seed + int64(m.ID)*1000003 + int64(len(existing))))
			}
			choices, err := selector.SelectWithExisting(candidates, existing, o, e.policy, rng)
			if err != nil {
				return fmt.Errorf("selection at %d ms: %w", t, err)
			}
			eligible, _ := selector.Eligible(candidates, o)
			for _, d := range choices {
				i := indices[d.ID]
				a := &assignment{allocated: true}
				m.assigned[i] = a
				e.relays[i].used += e.c.MessageBytes
				m.Recipients = append(m.Recipients, d.ID)
				e.stats.MaxCopies = max(e.stats.MaxCopies, len(m.Recipients)+1)
				if e.trace {
					assess := []selector.Assessment{}
					for _, c := range eligible {
						assignedBefore := false
						for _, old := range existing {
							assignedBefore = assignedBefore || c.ID == old.ID
						}
						if !assignedBefore {
							v := selector.Assess(c, existing, o)
							if e.policy != selector.RiskAware {
								v.Score = v.Base
							}
							assess = append(assess, v)
						}
					}
					limit := e.c.Budget
					if e.policy == selector.All {
						limit = len(e.relays)
					}
					e.result.Events = append(e.result.Events, Event{Time: t, Kind: "assigned", Message: m.ID, Node: d.ID, Detail: fmt.Sprintf("Получатель закреплён в бюджете: %d из %d", len(m.Recipients), limit), Candidates: assess, Chosen: []selector.Decision{d}})
				}
				existing = append(existing, e.candidate(i))
			}
		}
		for i, a := range m.assigned {
			if e.sourceActive >= 2 {
				break
			}
			if a == nil || a.localAcked || a.localBusy || a.nextLocal > t || !e.localUp(i, t, t) {
				continue
			}
			r := e.c.Relays[i]
			end := t + duration(e.c.MessageBytes, r.LocalBytesPerSec, r.LatencyMS)
			if end > m.Deadline {
				continue
			}
			if !a.allocated {
				if e.relays[i].used+e.c.MessageBytes > r.CapacityBytes {
					continue
				}
				a.allocated = true
				e.relays[i].used += e.c.MessageBytes
			}
			a.localBusy = true
			a.localAttempts++
			e.sourceActive++
			e.stats.LocalAttempts++
			e.stats.DataBytes += e.c.MessageBytes
			if a.localAttempts > 1 {
				e.stats.Retries++
			}
			e.startFlight(pending{at: end, kind: localData, m: m, relay: i, start: t, attempt: a.localAttempts}, "source", r.ID)
		}
	}
	for i, r := range e.c.Relays {
		if e.relays[i].busy || !e.upstreamUp(i, t, t) {
			continue
		}
		for _, m := range e.messages {
			a := m.assigned[i]
			if m.done || a == nil || !a.stored || a.upBusy || a.nextUp > t {
				continue
			}
			end := t + duration(e.c.MessageBytes, r.UplinkBytesPerSec, r.LatencyMS)
			if end > m.Deadline {
				continue
			}
			e.relays[i].busy = true
			a.upBusy = true
			a.upAttempts++
			e.stats.ForwardAttempts++
			e.stats.DataBytes += e.c.MessageBytes
			if a.upAttempts > 1 {
				e.stats.Retries++
			}
			e.startFlight(pending{at: end, kind: uplinkData, m: m, relay: i, start: t, attempt: a.upAttempts}, r.ID, "collector")
			break
		}
	}
	return nil
}

func (e *engine) startFlight(x pending, from, to string) {
	// Flights extending past the horizon still consume capacity in the final frame.
	e.seq++
	x.seq = e.seq
	e.flights[x.seq] = Flight{From: from, To: to, Message: x.m.ID, Start: x.start, End: x.at}
	if e.trace {
		e.result.Transfers = append(e.result.Transfers, e.flights[x.seq])
	}
	if x.at <= e.c.DurationMS {
		heap.Push(&e.q, x)
	}
	e.log(x.start, "transfer", x.m, to, "Начата передача "+from+" → "+to)
}

func (e *engine) snapshotStats() Stats {
	s := e.stats
	s.Pending = s.Generated - s.Delivered - s.Expired - s.Rejected
	if s.Generated > 0 {
		s.DeliveryRate = float64(s.Delivered) / float64(s.Generated)
	}
	latencies := make([]int64, 0, s.Delivered)
	for _, m := range e.messages {
		if m.DeliveredAt >= 0 {
			latencies = append(latencies, m.DeliveredAt-m.Created)
		}
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		s.P50MS = latencies[(len(latencies)-1)/2]
		s.P95MS = latencies[(len(latencies)*95+99)/100-1]
	}
	return s
}

func (e *engine) recordFrame(t int64) {
	f := Frame{Time: t, Stats: e.snapshotStats(), Nodes: []Node{{ID: "source", Up: true, UsedBytes: e.stats.SourceBytes, CapacityBytes: e.c.SourceCapacityBytes, Busy: e.sourceActive > 0}}, Flights: []Flight{}}
	for i, r := range e.c.Relays {
		n := Node{ID: r.ID, Up: e.localUp(i, t, t), UsedBytes: e.relays[i].used, CapacityBytes: r.CapacityBytes, Busy: e.relays[i].busy}
		for _, m := range e.messages {
			if a := m.assigned[i]; a != nil && a.allocated {
				if a.stored {
					n.Stored++
				} else {
					n.Reserved++
				}
			}
		}
		f.Nodes = append(f.Nodes, n)
	}
	f.Nodes = append(f.Nodes, Node{ID: "collector", Up: !blocked(e.c.CollectorOutages, t, t), Stored: e.stats.Delivered})
	keys := make([]int, 0, len(e.flights))
	for key := range e.flights {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	for _, key := range keys {
		f.Flights = append(f.Flights, e.flights[key])
	}
	e.result.Frames = append(e.result.Frames, f)
}
