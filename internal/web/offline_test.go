package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

func TestOfflineContainsActualGoReport(t *testing.T) {
	c, _ := sim.Preset("lost_ack")
	c.Title = `<script>alert("title")</script>`
	page, err := Offline(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(page, []byte(`<script src=`)) || bytes.Contains(page, []byte(`href="/style.css"`)) || bytes.Contains(page, []byte(c.Title)) {
		t.Fatal("offline file has external resources or an unescaped title")
	}
	match := regexp.MustCompile(`const raw=atob\("([A-Za-z0-9+/=]+)"\)`).FindSubmatch(page)
	if len(match) != 2 {
		t.Fatal("embedded replay is missing")
	}
	packed, err := base64.StdEncoding.DecodeString(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	want, err := sim.RunAll(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(data, wantJSON) {
		t.Fatal("offline report differs from the Go engine")
	}
	if !strings.Contains(string(page), "window.RELAY_OFFLINE=true") {
		t.Fatal("viewer does not identify its offline mode")
	}
}

func TestOfflineCancellation(t *testing.T) {
	c, _ := sim.Preset("shared_failure")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Offline(ctx, c); err == nil {
		t.Fatal("cancelled computation succeeded")
	}
}
