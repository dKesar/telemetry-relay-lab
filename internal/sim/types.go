package sim

import "github.com/dKesar/telemetry-relay-lab/internal/selector"

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
	Time    int64    `json:"time_ms"`
	Stats   Stats    `json:"stats"`
	Nodes   []Node   `json:"nodes"`
	Flights []Flight `json:"flights"`
}

type Event struct {
	Time       int64                 `json:"time_ms"`
	Kind       string                `json:"kind"`
	Message    int                   `json:"message"`
	Node       string                `json:"node"`
	Detail     string                `json:"detail"`
	Candidates []selector.Assessment `json:"candidates,omitempty"`
	Chosen     []selector.Decision   `json:"chosen,omitempty"`
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
