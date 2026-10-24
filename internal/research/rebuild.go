package research

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/dKesar/telemetry-relay-lab/internal/adaptive"
	"github.com/dKesar/telemetry-relay-lab/internal/selector"
	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

// Rebuild regenerates only derived summaries and graphics from saved raw rows.
// It does not rerun or certify the physical simulation. Raw rows are preserved.
func Rebuild(dest string) error {
	data, err := os.ReadFile(filepath.Join(dest, "summary.json"))
	if err != nil {
		return err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return err
	}
	if report.Model != sim.Version || report.Protocol != Protocol || report.Algorithm != adaptive.Revision || report.Runs < 2 || report.Runs > 100 {
		return fmt.Errorf("unsupported evidence version")
	}
	f, err := os.Open(filepath.Join(dest, "runs.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(io.LimitReader(f, 8<<20))
	header, err := reader.Read()
	if err != nil {
		return err
	}
	expected := []string{"case", "run", "seed", "policy", "generated", "delivered", "expired", "rejected", "pending", "delivery_rate", "data_bytes", "control_bytes", "probe_bytes", "assigned", "p95_ms", "evaluated_sets"}
	if fmt.Sprint(header) != fmt.Sprint(expected) {
		return fmt.Errorf("unexpected CSV schema")
	}
	var rows []Row
	seen := map[string]bool{}
	validCases := map[string]bool{}
	validPolicies := map[selector.Policy]bool{}
	for _, c := range Cases() {
		validCases[c.Name] = true
	}
	for _, p := range sim.ResearchPolicies() {
		validPolicies[p] = true
	}
	for {
		v, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		var r Row
		r.Case = v[0]
		r.Policy = selector.Policy(v[3])
		r.Run, err = strconv.Atoi(v[1])
		if err != nil {
			return err
		}
		r.Seed, err = strconv.ParseInt(v[2], 10, 64)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s/%d/%s", r.Case, r.Run, r.Policy)
		if seen[key] || !validCases[r.Case] || !validPolicies[r.Policy] || r.Run < 0 || r.Run >= report.Runs || r.Seed != report.Seed+int64(r.Run) {
			return fmt.Errorf("unexpected or duplicate raw row")
		}
		seen[key] = true
		ints := []*int{&r.Stats.Generated, &r.Stats.Delivered, &r.Stats.Expired, &r.Stats.Rejected, &r.Stats.Pending}
		for i, p := range ints {
			*p, err = strconv.Atoi(v[4+i])
			if err != nil || *p < 0 {
				return fmt.Errorf("invalid counters")
			}
		}
		r.Stats.DeliveryRate, err = strconv.ParseFloat(v[9], 64)
		if err != nil {
			return err
		}
		for i, p := range []*int64{&r.Stats.DataBytes, &r.Stats.ControlBytes, &r.Stats.ProbeBytes} {
			*p, err = strconv.ParseInt(v[10+i], 10, 64)
			if err != nil || *p < 0 {
				return fmt.Errorf("invalid bytes")
			}
		}
		r.Stats.Assigned, err = strconv.Atoi(v[13])
		if err != nil {
			return err
		}
		r.Stats.P95MS, err = strconv.ParseInt(v[14], 10, 64)
		if err != nil {
			return err
		}
		r.Stats.Evaluated, err = strconv.Atoi(v[15])
		if err != nil {
			return err
		}
		if r.Stats.Generated <= 0 || r.Stats.Generated != r.Stats.Delivered+r.Stats.Expired+r.Stats.Rejected+r.Stats.Pending {
			return fmt.Errorf("raw counters do not balance")
		}
		// Reconstruct the fraction from integer counters; do not lose precision
		// through the human-readable nine-decimal CSV field.
		r.Stats.DeliveryRate = float64(r.Stats.Delivered) / float64(r.Stats.Generated)
		rows = append(rows, r)
	}
	if len(rows) != len(Cases())*report.Runs*len(sim.ResearchPolicies()) || len(rows) != report.Checks {
		return fmt.Errorf("incomplete raw data")
	}
	// Stable run order is essential for paired contrasts even if CSV rows move.
	orderRows(rows)
	report.Summary, report.Comparisons = summarize(rows, report.Runs, report.Seed^0x9e3779b9)
	data, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "summary.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := WriteEvidence(dest, report); err != nil {
		return err
	}
	return dataArchive(dest)
}
