// Package sim is a deterministic discrete-event model of a source, relays and a
// collector. It is a research simulator, not a deployment of network agents.
package sim

import (
	"fmt"
	"math"
)

const Version = "timeline-v2"
const TickMS int64 = 100
const FrameMS int64 = 200
const AckBytes int64 = 64

type Interval struct {
	Start int64 `json:"start_ms"`
	End   int64 `json:"end_ms"`
}

type Domain struct {
	ID      string     `json:"id"`
	Outages []Interval `json:"outages"`
}

type Relay struct {
	ID                string   `json:"id"`
	Availability      float64  `json:"availability"`
	ReportedGroups    []string `json:"reported_groups"`
	GroupsKnown       bool     `json:"groups_known"`
	PhysicalGroups    []string `json:"physical_groups"`
	CapacityBytes     int64    `json:"capacity_bytes"`
	LocalBytesPerSec  int64    `json:"local_bytes_per_second"`
	UplinkBytesPerSec int64    `json:"uplink_bytes_per_second"`
	LatencyMS         int64    `json:"latency_ms"`
	DropProbability   float64  `json:"drop_probability"`
}

type Config struct {
	Name                  string     `json:"name"`
	Title                 string     `json:"title"`
	Description           string     `json:"description"`
	Seed                  int64      `json:"seed"`
	DurationMS            int64      `json:"duration_ms"`
	GenerateUntilMS       int64      `json:"generate_until_ms"`
	IntervalMS            int64      `json:"interval_ms"`
	TTLMS                 int64      `json:"ttl_ms"`
	MessageBytes          int64      `json:"message_bytes"`
	SourceCapacityBytes   int64      `json:"source_capacity_bytes"`
	Budget                int        `json:"budget"`
	Lambda                float64    `json:"lambda"`
	UnknownPenalty        float64    `json:"unknown_penalty"`
	SourceDropProbability float64    `json:"source_drop_probability"`
	AckLossProbability    float64    `json:"ack_loss_probability"`
	AckDelayMS            int64      `json:"ack_delay_ms"`
	Domains               []Domain   `json:"domains"`
	CollectorOutages      []Interval `json:"collector_outages"`
	Relays                []Relay    `json:"relays"`
}

func probability(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 && x <= 1 }
func intervalsOK(xs []Interval, duration int64) bool {
	var last int64
	for _, x := range xs {
		if x.Start < last || x.Start < 0 || x.End <= x.Start || x.End > duration {
			return false
		}
		last = x.End
	}
	return true
}

func (c Config) Validate() error {
	if c.Name == "" || c.DurationMS < 1000 || c.DurationMS > 120000 || c.GenerateUntilMS <= 0 || c.GenerateUntilMS > c.DurationMS || c.IntervalMS < 50 || (c.GenerateUntilMS-1)/c.IntervalMS+1 > 1000 {
		return fmt.Errorf("invalid duration or workload (at most 120 s and 1000 messages)")
	}
	if c.TTLMS < 500 || c.TTLMS > 60000 || c.MessageBytes <= 0 || c.MessageBytes > 65536 || c.SourceCapacityBytes < 0 || c.SourceCapacityBytes > 64<<20 || c.AckDelayMS < 0 || c.AckDelayMS > 5000 {
		return fmt.Errorf("invalid storage, message size, TTL or ACK delay")
	}
	if len(c.Relays) == 0 || len(c.Relays) > 12 || c.Budget < 0 || c.Budget > len(c.Relays) || c.Lambda < 0 || c.Lambda > 10 || math.IsNaN(c.Lambda) || !probability(c.UnknownPenalty) || !probability(c.SourceDropProbability) || !probability(c.AckLossProbability) {
		return fmt.Errorf("invalid algorithm parameters")
	}
	groups := make(map[string]bool)
	for _, d := range c.Domains {
		if d.ID == "" || groups[d.ID] || !intervalsOK(d.Outages, c.DurationMS) {
			return fmt.Errorf("invalid domain %q", d.ID)
		}
		groups[d.ID] = true
	}
	if !intervalsOK(c.CollectorOutages, c.DurationMS) {
		return fmt.Errorf("invalid collector outage intervals")
	}
	ids := make(map[string]bool)
	for _, r := range c.Relays {
		if r.ID == "" || r.ID == "source" || r.ID == "collector" || len(r.ID) > 32 || ids[r.ID] || !probability(r.Availability) || !probability(r.DropProbability) || r.CapacityBytes < 0 || r.CapacityBytes > 64<<20 || r.LocalBytesPerSec <= 0 || r.LocalBytesPerSec > 1<<30 || r.UplinkBytesPerSec <= 0 || r.UplinkBytesPerSec > 1<<30 || r.LatencyMS < 1 || r.LatencyMS > 5000 {
			return fmt.Errorf("invalid relay %q", r.ID)
		}
		ids[r.ID] = true
		for _, list := range [][]string{r.ReportedGroups, r.PhysicalGroups} {
			seen := make(map[string]bool)
			for _, g := range list {
				if g == "" || seen[g] {
					return fmt.Errorf("invalid groups for %s", r.ID)
				}
				seen[g] = true
			}
		}
		for _, g := range r.PhysicalGroups {
			if !groups[g] {
				return fmt.Errorf("unknown physical group %q", g)
			}
		}
	}
	return nil
}

// Presets returns fresh values: callers can safely change or JSON-encode them.
func Presets() []Config {
	base := func() Config {
		return Config{
			Name: "shared_failure", Title: "Общий отказ", Description: "Сначала копии накапливаются. На 8-й секунде общий сегмент А и Б отключается, независимый путь В остаётся доступен.",
			Seed: 42, DurationMS: 45000, GenerateUntilMS: 30000, IntervalMS: 250, TTLMS: 12000, MessageBytes: 512, SourceCapacityBytes: 262144,
			Budget: 2, Lambda: 1, UnknownPenalty: .5, SourceDropProbability: .01, AckLossProbability: .03, AckDelayMS: 80,
			Domains: []Domain{{ID: "AP1", Outages: []Interval{{8000, 24000}}}, {ID: "AP2", Outages: []Interval{}}}, CollectorOutages: []Interval{{0, 8000}},
			Relays: []Relay{
				{ID: "A", Availability: .72, ReportedGroups: []string{"AP1"}, GroupsKnown: true, PhysicalGroups: []string{"AP1"}, CapacityBytes: 262144, LocalBytesPerSec: 16384, UplinkBytesPerSec: 8192, LatencyMS: 40, DropProbability: .05},
				{ID: "B", Availability: .68, ReportedGroups: []string{"AP1"}, GroupsKnown: true, PhysicalGroups: []string{"AP1"}, CapacityBytes: 262144, LocalBytesPerSec: 16384, UplinkBytesPerSec: 8192, LatencyMS: 50, DropProbability: .08},
				{ID: "C", Availability: .56, ReportedGroups: []string{"AP2"}, GroupsKnown: true, PhysicalGroups: []string{"AP2"}, CapacityBytes: 262144, LocalBytesPerSec: 16384, UplinkBytesPerSec: 8192, LatencyMS: 70, DropProbability: .12},
			},
		}
	}
	out := []Config{base()}
	independent := base()
	independent.Name = "independent"
	independent.Title = "Без общего отказа"
	independent.Description = "Общий сегмент не отключается. Независимые потери пакетов и более медленная альтернатива позволяют увидеть цену резервирования."
	independent.Domains[0].Outages = []Interval{}
	out = append(out, independent)
	weak := base()
	weak.Name = "weak_alternative"
	weak.Title = "Слабая альтернатива"
	weak.Description = "У В низкая оценка доступности и медленный канал. Мягкий штраф вправе предпочесть А и Б; жёсткое разнесение не обязано выиграть."
	weak.Relays[2].Availability = .08
	weak.Relays[2].DropProbability = .8
	weak.Relays[2].UplinkBytesPerSec = 1024
	out = append(out, weak)
	wrong := base()
	wrong.Name = "wrong_labels"
	wrong.Title = "Ошибочные метки"
	wrong.Description = "Физически общий отказ по-прежнему у А и Б. Но алгоритму ошибочно сообщили, что общая зависимость у А и В."
	wrong.Relays[0].ReportedGroups = []string{"X"}
	wrong.Relays[1].ReportedGroups = []string{"Y"}
	wrong.Relays[2].ReportedGroups = []string{"X"}
	out = append(out, wrong)
	overload := base()
	overload.Name = "overload"
	overload.Title = "Перегрузка очередей"
	overload.Description = "Небольшие очереди и поток 20 сообщений в секунду. Отклонения при перегрузке остаются в знаменателе итоговой доли доставки."
	overload.IntervalMS = 50
	overload.SourceCapacityBytes = 8192
	for i := range overload.Relays {
		overload.Relays[i].CapacityBytes = 4096
		overload.Relays[i].UplinkBytesPerSec = 2048
	}
	out = append(out, overload)
	ack := base()
	ack.Name = "lost_ack"
	ack.Title = "Потеря квитанций"
	ack.Description = "Данные могут быть доставлены, а подтверждение потеряться. Повтор остаётся у прежнего получателя; бюджет копий не выдаётся заново."
	ack.AckLossProbability = .65
	out = append(out, ack)
	return out
}

func Preset(name string) (Config, error) {
	for _, c := range Presets() {
		if c.Name == name {
			return c, nil
		}
	}
	return Config{}, fmt.Errorf("unknown preset %q", name)
}
