package main

import (
	"bytes"
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
