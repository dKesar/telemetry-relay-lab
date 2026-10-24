package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

// Offline packages a real Go-generated replay in one HTML file. Parameters are
// intentionally read-only: this viewer never pretends to run Go in JavaScript.
func Offline(ctx context.Context, c sim.Config) ([]byte, error) {
	report, err := sim.RunAll(ctx, c, true)
	if err != nil {
		return nil, err
	}
	return packOffline(report)
}

// OfflineSuite lets an exhibition replay several computed cases without Go.
func OfflineSuite(ctx context.Context, configs []sim.Config) ([]byte, error) {
	if len(configs) < 1 || len(configs) > 9 {
		return nil, fmt.Errorf("offline suite requires 1..9 cases")
	}
	var reports []sim.Report
	seen := map[string]bool{}
	for _, c := range configs {
		if seen[c.Name] {
			return nil, fmt.Errorf("duplicate case")
		}
		seen[c.Name] = true
		r, err := sim.RunAll(ctx, c, true)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return packOffline(reports)
}

func packOffline(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var zipped bytes.Buffer
	z := gzip.NewWriter(&zipped)
	if _, err := z.Write(data); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	html, _ := assets.ReadFile("assets/index.html")
	css, _ := assets.ReadFile("assets/style.css")
	js, _ := assets.ReadFile("assets/app.js")
	bootstrap := `"use strict";window.RELAY_OFFLINE=true;
const offlineReport=(async()=>{
  if(typeof DecompressionStream==="undefined")throw new Error("Для офлайн-повтора нужен современный Chrome, Firefox или Safari.");
  const raw=atob("` + base64.StdEncoding.EncodeToString(zipped.Bytes()) + `");
  const bytes=Uint8Array.from(raw,c=>c.charCodeAt(0));
  return JSON.parse(await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"))).text());
})();
window.fetch=async(path,options)=>{
  const value=await offlineReport,reports=Array.isArray(value)?value:[value];
  if(path==="/api/presets")return {ok:true,json:async()=>reports.map(r=>r.config)};
  if(path==="/api/run"){const name=JSON.parse(options.body).config.name,report=reports.find(r=>r.config.name===name);if(!report)throw new Error("Сценарий отсутствует в файле");return {ok:true,json:async()=>report};}
  throw new Error("Сетевые запросы в офлайн-повторе отключены");
};`
	out := strings.Replace(string(html), `  <link rel="stylesheet" href="/style.css">`, "  <style>"+string(css)+"</style>", 1)
	out = strings.Replace(out, `  <script src="/app.js" defer></script>`, "", 1)
	out = strings.Replace(out, `Локальный стенд`, "Офлайн-повтор", 1)
	out = strings.Replace(out, `Решения и события рассчитывает Go.<br>Браузер воспроизводит журнал опыта.`, `Этот опыт уже рассчитан на Go.<br>Браузер воспроизводит сохранённый журнал.`, 1)
	out = strings.Replace(out, "</body>", "<script>"+bootstrap+"\n"+string(js)+"</script></body>", 1)
	return []byte(out), nil
}
