package sim

import (
	"github.com/dKesar/telemetry-relay-lab/internal/adaptive"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
)

const Adaptive selector.Policy = "adaptive"
const AdaptiveIndependent selector.Policy = "adaptive_independent"
const AdaptiveNoDeadline selector.Policy = "adaptive_no_deadline"
const AdaptiveStatic selector.Policy = "adaptive_static"

func Policies() []selector.Policy { return append(selector.Policies(), Adaptive) }
func ResearchPolicies() []selector.Policy {
	return append(Policies(), AdaptiveIndependent, AdaptiveNoDeadline, AdaptiveStatic)
}
func adaptivePolicy(p selector.Policy) bool {
	return p == Adaptive || p == AdaptiveIndependent || p == AdaptiveNoDeadline || p == AdaptiveStatic
}

type Stats struct {
	Generated       int     `json:"generated"`
	Delivered       int     `json:"delivered"`
	Expired         int     `json:"expired"`
	Rejected        int     `json:"rejected"`
	Pending         int     `json:"pending"`
	DeliveryRate    float64 `json:"delivery_rate"`
	LocalAttempts   int     `json:"local_attempts"`
	ForwardAttempts int     `json:"forward_attempts"`
	Retries         int     `json:"retries"`
	Duplicates      int     `json:"duplicates"`
	LocalDuplicates int     `json:"local_duplicates"`
	DataBytes       int64   `json:"data_bytes"`
	ControlBytes    int64   `json:"control_bytes"`
	SourceBytes     int64   `json:"source_bytes"`
	MaxCopies       int     `json:"max_copies"`
	P50MS           int64   `json:"p50_ms"`
	P95MS           int64   `json:"p95_ms"`
	ProbeBytes      int64   `json:"probe_bytes"`
	Assigned        int     `json:"assigned"`
	Decisions       int     `json:"decisions"`
	Evaluated       int     `json:"evaluated_sets"`
}

type Node struct {
	ID            string `json:"id"`
	Up            bool   `json:"up"`
	UsedBytes     int64  `json:"used_bytes"`
	CapacityBytes int64  `json:"capacity_bytes"`
	Stored        int    `json:"stored"`
	Reserved      int    `json:"reserved"`
	Busy          bool   `json:"busy"`
}

type Flight struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Message int    `json:"message"`
	Start   int64  `json:"start_ms"`
	End     int64  `json:"end_ms"`
}

type Frame struct {
	Time    int64             `json:"time_ms"`
	Stats   Stats             `json:"stats"`
	Nodes   []Node            `json:"nodes"`
	Flights []Flight          `json:"flights"`
	Health  []adaptive.Health `json:"health,omitempty"`
	Pairs   []adaptive.Pair   `json:"pairs,omitempty"`
}

type Event struct {
	Time       int64                 `json:"time_ms"`
	Kind       string                `json:"kind"`
	Message    int                   `json:"message"`
	Node       string                `json:"node"`
	Detail     string                `json:"detail"`
	Candidates []selector.Assessment `json:"candidates,omitempty"`
	Chosen     []selector.Decision   `json:"chosen,omitempty"`
	Plan       *adaptive.Plan        `json:"plan,omitempty"`
}

type Message struct {
	ID          int      `json:"id"`
	Created     int64    `json:"created_ms"`
	Deadline    int64    `json:"deadline_ms"`
	DeliveredAt int64    `json:"delivered_at_ms"`
	Recipients  []string `json:"recipients"`
	Rejected    bool     `json:"rejected"`
	Expired     bool     `json:"expired"`
}

type Result struct {
	Policy    selector.Policy `json:"policy"`
	Stats     Stats           `json:"stats"`
	Frames    []Frame         `json:"frames"`
	Events    []Event         `json:"events"`
	Messages  []Message       `json:"messages"`
	Transfers []Flight        `json:"transfers"`
}

type Report struct {
	Version string   `json:"version"`
	Config  Config   `json:"config"`
	Results []Result `json:"results"`
}
