package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactCLI(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"exact", "-config", "../../configs/paper.json"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shared_failure,risk_aware,A+C,2,548/625,0.876800000") {
		t.Fatal("missing exact reference result")
	}
}

func TestExperimentOutputsAndNoOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output")
	args := []string{"experiment", "-config", "../../configs/paper.json", "-runs", "2", "-trials", "100", "-out", dir}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "manifest.json", "runs.csv", "summary.csv", "exact.csv"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if err := run(args, &bytes.Buffer{}); err == nil {
		t.Fatal("overwrote existing experiment")
	}
}

func TestBadCLI(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"exact", "-config", "missing.json"}, {"exact", "extra"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestTimelineOutputs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "timeline")
	if err := run([]string{"timeline", "-runs", "2", "-seed", "64", "-out", dir}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "report.json", "runs.csv", "summary.csv", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	meta, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var manifest struct {
		Hash string `json:"config_sha256"`
	}
	if err := json.Unmarshal(meta, &manifest); err != nil || manifest.Hash != fmt.Sprintf("%x", sha256.Sum256(config)) {
		t.Fatal("manifest hash does not identify the saved config")
	}
	roundtrip := filepath.Join(t.TempDir(), "roundtrip")
	if err := run([]string{"timeline", "-config", filepath.Join(dir, "config.json"), "-out", roundtrip}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	second, _ := os.ReadFile(filepath.Join(roundtrip, "report.json"))
	if !bytes.Equal(first, second) {
		t.Fatal("saved configuration did not reproduce the first report")
	}
	if err := run([]string{"timeline", "-out", dir}, &bytes.Buffer{}); err == nil {
		t.Fatal("timeline overwrote results")
	}
	for _, args := range [][]string{{"timeline", "-preset", "unknown"}, {"timeline", "-runs", "0"}, {"serve", "-addr", "0.0.0.0:0"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatal("bad arguments accepted")
		}
	}
}

func TestOfflineCLIAndNoOverwrite(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "demo.html")
	args := []string{"offline", "-preset", "lost_ack", "-out", dest}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !bytes.Contains(data, []byte("RELAY_OFFLINE=true")) {
		t.Fatal("standalone HTML not saved", err)
	}
	if err := run(args, &bytes.Buffer{}); err == nil {
		t.Fatal("existing demo was overwritten")
	}
}
