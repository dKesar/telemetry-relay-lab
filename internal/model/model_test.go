package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	suite, _, err := Load("../../configs/paper.json")
	if err != nil || len(suite.Scenarios) != 4 {
		t.Fatalf("fixture: %v", err)
	}
	for _, raw := range []string{"-0.01", "1.01", "NaN", "1/0", ""} {
		if _, err := Probability(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"0", "1", "0.72", "18/25"} {
		if _, err := Probability(raw); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
	s := suite.Scenarios[0]
	s.Relays[0].ActualGroups = []string{"missing"}
	if err := s.Validate(); err == nil {
		t.Fatal("accepted nonexistent physical group")
	}
}

func TestStrictJSON(t *testing.T) {
	_, data, err := Load("../../configs/paper.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown":  strings.Replace(string(data), "\"schema_version\"", "\"typo\"", 1),
		"trailing": string(data) + "{}",
		"empty":    "{\"schema_version\":1,\"scenarios\":[]}",
	} {
		path := filepath.Join(t.TempDir(), name+".json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(path); err == nil {
			t.Fatalf("accepted %s JSON", name)
		}
	}
}

func TestWorldSharedDependencies(t *testing.T) {
	suite, _, _ := Load("../../configs/paper.json")
	w := suite.Scenarios[0].World()
	// AP1 down, AP2 up; all conditional delivery events up.
	got := w.Outcomes([]bool{false, true, true, true, true})
	if got[0] || got[1] || !got[2] {
		t.Fatalf("shared outage not respected: %v", got)
	}
}
