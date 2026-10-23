package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/dKesar/telemetry-relay-lab/internal/experiment"
	"github.com/dKesar/telemetry-relay-lab/internal/sim"
	"github.com/dKesar/telemetry-relay-lab/internal/web"
)

func serve(args []string, out io.Writer) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := f.String("addr", "127.0.0.1:8080", "loopback listen address")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return web.Serve(ctx, *addr, out)
}

func offline(args []string, out io.Writer) error {
	f := flag.NewFlagSet("offline", flag.ContinueOnError)
	preset := f.String("preset", "shared_failure", "saved scenario")
	seed := f.Int64("seed", 42, "seed")
	dest := f.String("out", "results/demo.html", "new standalone HTML file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	c, err := sim.Preset(*preset)
	if err != nil {
		return err
	}
	c.Seed = *seed
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	data, err := web.Offline(ctx, c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*dest), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(*dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprintln(out, "Saved offline replay:", *dest)
	return nil
}

func timeline(args []string, out io.Writer) error {
	f := flag.NewFlagSet("timeline", flag.ContinueOnError)
	preset := f.String("preset", "shared_failure", "shared_failure|independent|weak_alternative|wrong_labels|overload|lost_ack")
	configPath := f.String("config", "", "optional timeline Config JSON (not the snapshot suite)")
	seed := f.Int64("seed", 42, "base seed")
	runs := f.Int("runs", 1, "replications (1..100)")
	dest := f.String("out", "results/timeline", "new output directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *runs < 1 || *runs > 100 {
		return fmt.Errorf("invalid arguments or runs")
	}
	c, err := sim.Preset(*preset)
	if err != nil {
		return err
	}
	if *configPath != "" {
		file, err := os.Open(*configPath)
		if err != nil {
			return err
		}
		defer file.Close()
		d := json.NewDecoder(io.LimitReader(file, 64<<10))
		d.DisallowUnknownFields()
		var supplied sim.Config
		if err := d.Decode(&supplied); err != nil {
			return err
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return fmt.Errorf("one config JSON required")
		}
		c = supplied
		seedGiven := false
		f.Visit(func(flag *flag.Flag) { seedGiven = seedGiven || flag.Name == "seed" })
		if !seedGiven {
			*seed = c.Seed
		}
	}
	c.Seed = *seed
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*dest), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(*dest, 0755); err != nil {
		return fmt.Errorf("choose a new -out: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	records := [][]string{{"run", "seed", "policy", "generated", "delivered", "expired", "rejected", "pending", "delivery_rate", "data_bytes", "control_bytes", "retries", "duplicates", "latency_p50_ms", "latency_p95_ms"}}
	configJSON, _ := json.MarshalIndent(c, "", "  ")
	configJSON = append(configJSON, '\n')
	if err := os.WriteFile(filepath.Join(*dest, "config.json"), configJSON, 0644); err != nil {
		return err
	}
	var paired [][]experiment.RunResult
	for i := 0; i < *runs; i++ {
		c.Seed = *seed + int64(i)
		r, err := sim.RunAll(ctx, c, i == 0)
		if err != nil {
			return err
		}
		if i == 0 {
			data, err := json.Marshal(r)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(*dest, "report.json"), append(data, '\n'), 0644); err != nil {
				return err
			}
		}
		var rates []experiment.RunResult
		for _, v := range r.Results {
			s := v.Stats
			rates = append(rates, experiment.RunResult{Policy: v.Policy, Seed: c.Seed, Trials: s.Generated, Delivered: s.Delivered})
			records = append(records, []string{strconv.Itoa(i), strconv.FormatInt(c.Seed, 10), string(v.Policy), strconv.Itoa(s.Generated), strconv.Itoa(s.Delivered), strconv.Itoa(s.Expired), strconv.Itoa(s.Rejected), strconv.Itoa(s.Pending), number(s.DeliveryRate), strconv.FormatInt(s.DataBytes, 10), strconv.FormatInt(s.ControlBytes, 10), strconv.Itoa(s.Retries), strconv.Itoa(s.Duplicates), strconv.FormatInt(s.P50MS, 10), strconv.FormatInt(s.P95MS, 10)})
			fmt.Fprintf(out, "run=%d %-14s delivered=%d/%d expired=%d rejected=%d bytes=%d\n", i, v.Policy, s.Delivered, s.Generated, s.Expired, s.Rejected, s.DataBytes+s.ControlBytes)
		}
		paired = append(paired, rates)
	}
	if err := writeCSV(filepath.Join(*dest, "runs.csv"), func(w *csv.Writer) error { return w.WriteAll(records) }); err != nil {
		return err
	}
	if *runs >= 2 {
		summary, err := experiment.Summarize(paired, *seed^0x6a09e667)
		if err != nil {
			return err
		}
		rows := [][]string{{"policy", "runs", "mean_rate", "ci95_low", "ci95_high", "delta_vs_availability", "delta_ci95_low", "delta_ci95_high"}}
		for _, s := range summary {
			rows = append(rows, []string{string(s.Policy), strconv.Itoa(*runs), number(s.Mean), number(s.Lower), number(s.Upper), number(s.DeltaMean), number(s.DeltaLower), number(s.DeltaUpper)})
		}
		if err := writeCSV(filepath.Join(*dest, "summary.csv"), func(w *csv.Writer) error { return w.WriteAll(rows) }); err != nil {
			return err
		}
	}
	meta := map[string]any{"model": sim.Version, "config_sha256": fmt.Sprintf("%x", sha256.Sum256(configJSON)), "base_seed": *seed, "runs": *runs, "go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "report": "first run contains full replay; runs.csv contains all replications"}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(*dest, "manifest.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Fprintln(out, "Saved:", *dest)
	return nil
}
