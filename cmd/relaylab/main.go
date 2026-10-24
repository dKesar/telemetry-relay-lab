// relaylab runs reference calculations, time simulations and a visual research lab.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/dKesar/telemetry-relay-lab/internal/experiment"
	"github.com/dKesar/telemetry-relay-lab/internal/model"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "relaylab:", err)
		os.Exit(1)
	}
}

func number(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 9, 64)
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: relaylab serve|timeline|offline|research|exact|experiment (see README)")
	}
	command := args[0]
	if command == "serve" {
		return serve(args[1:], out)
	}
	if command == "timeline" {
		return timeline(args[1:], out)
	}
	if command == "offline" {
		return offline(args[1:], out)
	}
	if command == "research" {
		return researchCommand(args[1:], out)
	}
	if command != "exact" && command != "experiment" {
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	config := flags.String("config", "configs/paper.json", "scenario suite JSON")
	seed := flags.Int64("seed", 42, "base RNG seed")
	runs := flags.Int("runs", 30, "independent replications (2..1000)")
	trials := flags.Int("trials", 10000, "message snapshots per run (1..10000000)")
	dest := flags.String("out", "results/paper", "new output directory; existing paths are never overwritten")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	suite, raw, err := model.Load(*config)
	if err != nil {
		return err
	}
	if command == "exact" {
		return writeExact(out, suite)
	}
	if *runs < 2 || *runs > 1000 || *trials <= 0 || *trials > 10000000 {
		return fmt.Errorf("invalid runs or trials")
	}
	if err := os.MkdirAll(filepath.Dir(*dest), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(*dest, 0755); err != nil {
		return fmt.Errorf("choose a new -out directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(*dest, "config.json"), raw, 0644); err != nil {
		return err
	}
	manifest := struct {
		SchemaVersion    int    `json:"schema_version"`
		Model            string `json:"model"`
		BaseSeed         int64  `json:"base_seed"`
		Runs             int    `json:"runs"`
		Trials           int    `json:"trials_per_run"`
		ConfigSHA256     string `json:"config_sha256"`
		Go               string `json:"go_version"`
		OS               string `json:"goos"`
		Arch             string `json:"goarch"`
		BootstrapSamples int    `json:"bootstrap_samples"`
	}{1, "preplaced_snapshot_v1", *seed, *runs, *trials, fmt.Sprintf("%x", sha256.Sum256(raw)), runtime.Version(), runtime.GOOS, runtime.GOARCH, 2000}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*dest, "manifest.json"), append(manifestJSON, '\n'), 0644); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(*dest, "exact.csv"), func(w *csv.Writer) error { return exactRows(w, suite) }); err != nil {
		return err
	}
	runRecords := [][]string{{"scenario", "policy", "run", "seed", "trials", "delivered", "delivery_rate", "copies", "placement_payload_bytes_per_message"}}
	summaryRecords := [][]string{{"scenario", "policy", "runs", "mean_rate", "ci95_low", "ci95_high", "delta_vs_availability", "delta_ci95_low", "delta_ci95_high"}}
	for si, s := range suite.Scenarios {
		var results [][]experiment.RunResult
		for i := 0; i < *runs; i++ {
			runSeed := *seed + int64(si)*1000003 + int64(i)
			rows, err := experiment.Run(s, runSeed, *trials)
			if err != nil {
				return err
			}
			results = append(results, rows)
			for _, r := range rows {
				runRecords = append(runRecords, []string{s.Name, string(r.Policy), strconv.Itoa(i), strconv.FormatInt(runSeed, 10), strconv.Itoa(r.Trials), strconv.Itoa(r.Delivered), number(r.Rate()), strconv.Itoa(r.Copies), strconv.FormatInt(r.PlacementBytesPerMessage, 10)})
			}
		}
		summary, err := experiment.Summarize(results, *seed^int64(si)^0x6a09e667)
		if err != nil {
			return err
		}
		for _, r := range summary {
			summaryRecords = append(summaryRecords, []string{s.Name, string(r.Policy), strconv.Itoa(*runs), number(r.Mean), number(r.Lower), number(r.Upper), number(r.DeltaMean), number(r.DeltaLower), number(r.DeltaUpper)})
			fmt.Fprintf(out, "%-22s %-14s rate=%6.2f%% delta=%+6.2f pp\n", s.Name, r.Policy, r.Mean*100, r.DeltaMean*100)
		}
	}
	if err := writeCSV(filepath.Join(*dest, "runs.csv"), func(w *csv.Writer) error { return w.WriteAll(runRecords) }); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(*dest, "summary.csv"), func(w *csv.Writer) error { return w.WriteAll(summaryRecords) }); err != nil {
		return err
	}
	fmt.Fprintln(out, "Saved:", *dest)
	return nil
}

func writeCSV(path string, fn func(*csv.Writer) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	writeErr := fn(w)
	w.Flush()
	if writeErr == nil {
		writeErr = w.Error()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeExact(out io.Writer, suite model.Suite) error {
	w := csv.NewWriter(out)
	err := exactRows(w, suite)
	w.Flush()
	if err != nil {
		return err
	}
	return w.Error()
}

func exactRows(w *csv.Writer, suite model.Suite) error {
	if err := w.Write([]string{"scenario", "policy", "recipients", "copies", "delivery_probability_fraction", "delivery_probability"}); err != nil {
		return err
	}
	for _, s := range suite.Scenarios {
		rows, err := experiment.Exact(s)
		if err != nil {
			return err
		}
		for _, r := range rows {
			ids := strings.Join(r.Recipients, "+")
			if r.Policy == "random" {
				ids = "uniform_K_subset"
			}
			if err := w.Write([]string{s.Name, string(r.Policy), ids, strconv.Itoa(r.Copies), r.Probability.RatString(), r.Probability.FloatString(9)}); err != nil {
				return err
			}
		}
	}
	return nil
}
