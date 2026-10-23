"use strict";

// The algorithm runs in Go. JavaScript only requests and replays a report.
const $ = (id) => document.getElementById(id);
const labels = { random: "Случайный выбор", availability: "Без штрафа за риск", diverse: "Жёсткое разнесение", risk_aware: "С учётом риска", all: "Всем (больший бюджет)" };
const kinds = { assigned: "Назначение", created: "Создано", transfer: "Передача", stored: "Сохранено", delivered: "Доставлено", duplicate: "Дубликат", ack: "Квитанция", ack_lost: "Потеря ACK", receipt: "Получен ACK", expired: "Просрочено", rejected: "Отклонено", lost: "Потеря" };
const colors = { availability: "#b88a63", risk_aware: "#4f8052", random: "#8b8f9c", diverse: "#7599b0", all: "#927b9c" };
let presets = [], config = null, report = null, current = 0, playing = false, lastTick = 0, requestNumber = 0;
let leftPolicy = "availability", rightPolicy = "risk_aware", lastFrameKey = "";
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const seconds = (t) => (t / 1000).toFixed(1);
const bytes = (n) => n < 1024 ? `${n} Б` : `${(n / 1024).toFixed(1)} КиБ`;
const percent = (n) => (n * 100).toFixed(1) + "%";
const clone = (value) => JSON.parse(JSON.stringify(value));
const svgText = (x, y, text, css = "topo-sub", anchor = "middle") => `<text x="${x}" y="${y}" class="${css}" text-anchor="${anchor}">${esc(text)}</text>`;

function showError(message) { $("error").hidden = !message; $("error").textContent = message; }
function stop() { playing = false; $("play").textContent = "▶ Воспроизвести"; }

function pickPreset(name) {
  const chosen = presets.find(p => p.name === name);
  if (!chosen) return;
  stop(); config = clone(chosen);
  $("seed").value = config.seed; $("budget").value = config.budget; $("lambda").value = config.lambda;
  $("ttl").value = config.ttl_ms / 1000; $("rate").value = Math.round(1000 / config.interval_ms);
  $("scenarioName").textContent = config.title; $("description").textContent = config.description;
  for (const b of $("presets").children) { b.classList.toggle("active", b.dataset.name === name); b.setAttribute("aria-pressed", String(b.dataset.name === name)); }
  build();
}

async function build() {
  if (!config) return;
  stop(); showError("");
  const id = ++requestNumber;
  const next = clone(config);
  next.seed = Number($("seed").value); next.budget = Number($("budget").value); next.lambda = Number($("lambda").value);
  next.ttl_ms = Math.round(Number($("ttl").value) * 1000); next.interval_ms = Math.round(1000 / Number($("rate").value));
  if (![next.seed, next.budget, next.lambda, next.ttl_ms, next.interval_ms].every(Number.isFinite)) { showError("Заполните все параметры числами."); return; }
  $("build").disabled = true; $("play").disabled = true; $("build").textContent = "Рассчитываем…";
  try {
    const response = await fetch("/api/run", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ config: next }) });
    if (!response.ok) throw new Error(await response.text());
    const value = await response.json();
    if (id !== requestNumber) return;
    hydrate(value);
  } catch (error) { if (id === requestNumber) { if (report) hydrate(report); showError("Не удалось рассчитать опыт: " + error.message); } }
  finally { if (id === requestNumber) { $("build").disabled = !!window.RELAY_OFFLINE; $("play").disabled = !report; $("build").innerHTML = window.RELAY_OFFLINE ? "Параметры сохранённого опыта" : "Пересчитать опыт <span>↗</span>"; } }
}

function hydrate(value) {
  if (!value || value.version !== "timeline-v2" || !value.config || !Array.isArray(value.config.relays) || !Array.isArray(value.results) || value.results.length !== 5) throw new Error("Ожидался журнал временной модели timeline-v2.");
  const cfg=value.config, finite=(v)=>typeof v==="number"&&Number.isFinite(v);
  // Go encodes absent, valid empty slices as null. Treat them as empty sets.
  cfg.domains ??= []; cfg.collector_outages ??= [];
  for(const relay of cfg.relays)relay.physical_groups ??= [];
  for(const domain of cfg.domains)domain.outages ??= [];
  if(!finite(cfg.duration_ms)||cfg.duration_ms<1000||cfg.duration_ms>120000||cfg.relays.length<1||cfg.relays.length>12||!Array.isArray(cfg.domains)||!Array.isArray(cfg.collector_outages))throw new Error("Неверные параметры модели.");
  const expectedIDs=new Set(["source","collector",...cfg.relays.map(r=>r.id)]);
  if(expectedIDs.size!==cfg.relays.length+2||cfg.relays.some(r=>typeof r.id!=="string"||r.id.length>32||!Array.isArray(r.physical_groups)))throw new Error("Неверные идентификаторы узлов.");
  for(const domain of [...cfg.domains,{outages:cfg.collector_outages}])if(!Array.isArray(domain.outages)||domain.outages.some(o=>!finite(o.start_ms)||!finite(o.end_ms)||o.start_ms<0||o.end_ms<=o.start_ms||o.end_ms>cfg.duration_ms))throw new Error("Неверное расписание отказов.");
  const checkStats=(s)=>{const keys=["generated","delivered","expired","rejected","pending","delivery_rate","local_attempts","forward_attempts","retries","duplicates","data_bytes","control_bytes","source_bytes","p50_ms","p95_ms"];return s&&keys.every(k=>finite(s[k])&&s[k]>=0)&&s.generated===s.delivered+s.expired+s.rejected+s.pending};
  const seen=new Set();
  for (const r of value.results) {
    if (!Object.hasOwn(labels,r.policy) || !Array.isArray(r.frames) || r.frames.length < 1 || !Array.isArray(r.events) || !r.stats || r.frames.length > 2000 || r.events.length > 100000) throw new Error("Неверная структура журнала.");
    if(seen.has(r.policy)||!checkStats(r.stats))throw new Error("Неверные итоговые счётчики.");seen.add(r.policy);
    let previous=-1;
    for(const f of r.frames){
      if(!finite(f.time_ms)||f.time_ms<=previous||f.time_ms>cfg.duration_ms||!checkStats(f.stats)||!Array.isArray(f.nodes)||f.nodes.length!==expectedIDs.size||new Set(f.nodes.map(n=>n.id)).size!==expectedIDs.size||f.nodes.some(n=>!expectedIDs.has(n.id)||![n.used_bytes,n.capacity_bytes,n.stored,n.reserved].every(finite)))throw new Error("Неверный снимок состояния.");previous=f.time_ms;
    }
    if(r.frames[0].time_ms!==0||previous!==cfg.duration_ms)throw new Error("Журнал должен содержать начало и конец опыта.");
    for(const e of r.events)if(!finite(e.time_ms)||!Object.hasOwn(kinds,e.kind)||!Number.isInteger(e.message)||(e.candidates&&!e.candidates.every(c=>[c.base,c.risk,c.score].every(finite)))||(e.chosen&&!Array.isArray(e.chosen)))throw new Error("Неверное событие журнала.");
    for(const flight of (r.transfers||[]))if(!expectedIDs.has(flight.from)||!expectedIDs.has(flight.to)||!finite(flight.start_ms)||!finite(flight.end_ms)||flight.end_ms<=flight.start_ms)throw new Error("Неверная передача в журнале.");
  }
  report = value; config = clone(value.config); current = 0; lastFrameKey = "";
  $("time").max = config.duration_ms; $("time").value = 0; $("play").disabled = false;
  $("scenarioName").textContent = config.title; $("description").textContent = config.description;
  for(const b of $("presets").children){b.classList.toggle("active",b.dataset.name===config.name);b.setAttribute("aria-pressed",String(b.dataset.name===config.name))}
  $("seed").value = config.seed; $("budget").value = config.budget; $("lambda").value = config.lambda; $("ttl").value = config.ttl_ms / 1000; $("rate").value = Math.round(1000 / config.interval_ms);
  renderSummary(); render();
}

function result(policy) { return report.results.find(r => r.policy === policy); }
function frameAt(r) {
  let lo = 0, hi = r.frames.length - 1;
  while (lo < hi) { const mid = Math.ceil((lo + hi) / 2); if (r.frames[mid].time_ms <= current) lo = mid; else hi = mid - 1; }
  return r.frames[lo];
}

function topology(r, f) {
  const config = report.config;
  const relays = config.relays, height = Math.max(350, relays.length * 93 + 60), mid = height / 2;
  const coords = Object.assign(Object.create(null), { source: [72, mid], collector: [590, mid] });
  relays.forEach((n, i) => { coords[n.id] = [330, 75 + i * (height - 130) / Math.max(1, relays.length - 1)]; });
  const state = Object.fromEntries(f.nodes.map(n => [n.id, n]));
  let content = "";
  for (const domain of config.domains) {
    const members = relays.filter(n => n.physical_groups.includes(domain.id)); if (!members.length) continue;
    const ys = members.map(n => coords[n.id][1]), y = Math.min(...ys) - 32, h = Math.max(...ys) - Math.min(...ys) + 65;
    const down = domain.outages.some(x => current >= x.start_ms && current < x.end_ms);
    content += `<rect x="231" y="${y}" width="198" height="${h}" rx="10" class="topo-group ${down ? "off" : ""}"/>`;
    content += svgText(330, y - 7, `${domain.id} · ${down ? "ОТКЛЮЧЁН" : "общая зависимость"}`, "topo-group-label");
  }
  for (const relay of relays) {
    const [x, y] = coords[relay.id], off = !state[relay.id]?.up;
    const serverOff = off || !state.collector?.up;
    content += `<path d="M 119 ${mid} C 180 ${mid}, 195 ${y}, ${x - 62} ${y}" class="topo-edge ${off ? "off" : ""}"/>`;
    content += `<path d="M ${x + 62} ${y} C 465 ${y}, 480 ${mid}, 544 ${mid}" class="topo-edge ${serverOff ? "off" : ""}"/>`;
  }
  for (const node of f.nodes) {
    const point = coords[node.id]; if (!point) continue;
    const [x, y] = point, w = (node.id === "source" || node.id === "collector") ? 94 : 122;
    content += `<rect x="${x - w / 2}" y="${y - 25}" width="${w}" height="50" rx="7" class="topo-node ${node.up ? "" : "off"}"/>`;
    const name = node.id === "source" ? "Источник" : node.id === "collector" ? "Сервер" : `Узел ${node.id}`;
    let detail = node.id === "collector" ? `${node.stored} уникальных` : node.id === "source" ? bytes(node.used_bytes) : `${node.stored} копий · ${node.reserved} резерв`;
    if (!node.up) detail = "нет соединения";
    content += svgText(x, y - 3, name, "topo-title") + svgText(x, y + 13, detail);
  }
  const flights = (r.transfers || f.flights).filter(x => x.start_ms <= current && x.end_ms > current);
  for (const flight of flights) {
    const from = coords[flight.from], to = coords[flight.to]; if (!from || !to) continue;
    const u = Math.min(1, Math.max(0, (current - flight.start_ms) / (flight.end_ms - flight.start_ms)));
    const start = flight.from === "source" ? [from[0] + 47, from[1]] : [from[0] + 62, from[1]];
    const end = flight.to === "collector" ? [to[0] - 47, to[1]] : [to[0] - 62, to[1]];
    const cx1 = start[0] + (end[0] - start[0]) * .4, cx2 = start[0] + (end[0] - start[0]) * .6;
    const x = (1-u)**3 * start[0] + 3*(1-u)**2*u*cx1 + 3*(1-u)*u*u*cx2 + u**3*end[0];
    const y = (1-u)**3 * start[1] + 3*(1-u)**2*u*start[1] + 3*(1-u)*u*u*end[1] + u**3*end[1];
    content += `<circle cx="${x}" cy="${y}" r="4.5" class="topo-flow"><title>Сообщение ${flight.message}</title></circle>`;
  }
  return `<svg viewBox="0 0 662 ${height}" role="img" aria-label="Топология и состояние узлов">${content}</svg>`;
}

function renderPanel(id, policy, full) {
  const config = report.config;
  const element = $(id), r = result(policy), f = frameAt(r), s = f.stats;
  element.querySelector("h2").textContent = labels[policy];
  if (full) {
    element.querySelector(".metrics").innerHTML = `<div><div class="metric-label">Доставлено до срока</div><div class="metric-number">${s.delivered}<small> / ${s.generated}</small></div><div class="metric-foot">${percent(s.delivery_rate)} от созданных</div></div><div><div class="metric-label">Просрочено / отклонено</div><div class="metric-number danger">${s.expired}<small> / ${s.rejected}</small></div><div class="metric-foot">${s.pending} ещё ожидают</div></div><div><div class="metric-label">Передано данных + ACK</div><div class="metric-number">${((s.data_bytes + s.control_bytes)/1024).toFixed(1)}<small> КиБ</small></div><div class="metric-foot">${s.retries} повторов · ${s.duplicates} дублей</div></div>`;
    element.querySelector(".queue-list").innerHTML = config.relays.map(relay => {
      const n = f.nodes.find(x => x.id === relay.id), q = n.capacity_bytes ? Math.min(100, n.used_bytes / n.capacity_bytes * 100) : 0;
      // Use native progress elements for strict CSP and accessibility.
      return `<div><div class="queue-name"><span>Очередь ${esc(relay.id)}</span><span class="queue-count">${Math.round(q)}%</span></div><progress class="queue-progress" max="100" value="${q}" aria-label="Заполнение очереди ${esc(relay.id)}"></progress><div class="queue-detail">${n.stored} копий / ${n.reserved} резерв</div></div>`;
    }).join("");
  }
  element.querySelector(".topology").innerHTML = topology(r, f);
}

function chart() {
  const config = report.config;
  const width = 620, height = 194, left = 36, bottom = 160, top = 18, right = 602;
  const maxY = Math.max(10, ...report.results.map(r => r.stats.generated)), x = t => left + t / config.duration_ms * (right - left), y = n => bottom - n/maxY*(bottom-top);
  let c = "";
  for (const d of config.domains) for (const outage of d.outages) c += `<rect x="${x(outage.start_ms)}" y="${top}" width="${x(outage.end_ms)-x(outage.start_ms)}" height="${bottom-top}" fill="#f3e8dd"/>`;
  for (let i=0;i<=4;i++) { const value = maxY*i/4; c += `<line x1="${left}" x2="${right}" y1="${y(value)}" y2="${y(value)}" stroke="#e5ebde"/>` + svgText(left-7,y(value)+3,Math.round(value),"chart-label","end"); }
  for (let i=0;i<=3;i++) c += svgText(x(config.duration_ms*i/3),bottom+19,`${Math.round(config.duration_ms*i/3000)} с`,"chart-label");
  const selected = [...new Set([leftPolicy,rightPolicy])];
  for (const p of selected) {
    const points = result(p).frames.filter(f => f.time_ms<=current).map(f => `${x(f.time_ms)},${y(f.stats.delivered)}`).join(" ");
    c += `<polyline points="${points}" fill="none" stroke="${colors[p]}" stroke-width="2.4" stroke-linejoin="round"/>`;
  }
  c += `<line x1="${x(current)}" x2="${x(current)}" y1="${top}" y2="${bottom}" stroke="#8a9b7d" stroke-dasharray="3 4"/>`;
  selected.forEach((p,i) => { c += `<circle cx="${left+12+i*282}" cy="8" r="3" fill="${colors[p]}"/>` + svgText(left+21+i*282,11,labels[p],"chart-label","start"); });
  $("chart").innerHTML = `<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Число доставленных сообщений во времени">${c}</svg>`;
}

function inspect() {
  const decisions = result(rightPolicy).events.filter(e => e.kind === "assigned" && e.time_ms<=current);
  const d = decisions[decisions.length-1];
  if (!d) { $("decision").innerHTML = '<p class="empty">Нет назначений к текущему моменту. Увеличьте бюджет или перейдите вперёд.</p>'; $("decisionTime").textContent = "—"; return; }
  $("decisionTime").textContent = seconds(d.time_ms) + " с";
  const chosen = new Set(d.chosen.map(x => x.id));
  let formula = rightPolicy === "risk_aware" ? "score = base / (1 + λ × risk)" : rightPolicy === "diverse" ? "сначала min risk, затем max base" : rightPolicy === "random" ? "равновероятный набор допустимых узлов" : rightPolicy === "all" ? "все допустимые узлы" : "score = base; штраф отключён";
  $("decision").innerHTML = `<div class="formula">${esc(formula)}</div><div class="decision-title">Сообщение #${d.message} → выбран узел ${esc(d.node)}</div><table class="decision-table"><thead><tr><th>Узел</th><th>base</th><th>risk</th><th>score</th></tr></thead><tbody>${d.candidates.map(c => `<tr class="${chosen.has(c.id)?"chosen":""}"><td>${esc(c.id)} ${chosen.has(c.id)?"✓":""}</td><td>${c.base.toFixed(3)}</td><td>${c.risk.toFixed(2)}</td><td>${c.score.toFixed(3)}</td></tr>`).join("")}</tbody></table><p class="decision-note">${esc(d.detail)}. base учитывает доступность и текущую очередь. risk отражает сообщённые метки, которые могут быть ошибочными. Физические зависимости известны испытателю и не передаются алгоритму.</p>`;
}

function logs() {
  const allowed = new Set(["assigned","delivered","expired","lost","ack_lost","rejected","duplicate"]);
  const events = result(rightPolicy).events.filter(e => e.time_ms<=current && ($("verbose").checked || allowed.has(e.kind))).slice(-80).reverse();
  $("log").innerHTML = events.length ? events.map(e => `<div class="log-row"><span>${seconds(e.time_ms)} с</span><span class="event-kind ${esc(e.kind)}">${esc(kinds[e.kind] || e.kind)}</span><span>#${e.message}</span><span class="event-node">${esc(e.node)}</span><span class="event-detail">${esc(e.detail)}</span></div>`).join("") : '<p class="empty">В журнале выбранного справа метода пока нет событий.</p>';
}

function renderSummary() {
  $("summary").querySelector("tbody").innerHTML = report.results.map(r => { const s = r.stats; return `<tr class="${r.policy===rightPolicy?"selected-row":""}"><td>${esc(labels[r.policy])}</td><td>${s.delivered} / ${s.generated}</td><td>${percent(s.delivery_rate)}</td><td>${s.expired}</td><td>${s.rejected}</td><td>${s.pending}</td><td>${bytes(s.data_bytes+s.control_bytes)}</td><td>${s.delivered ? seconds(s.p95_ms) : "—"}</td></tr>`; }).join("");
}

function render() {
  if (!report) return;
  const config = report.config;
  current = Math.max(0,Math.min(current,config.duration_ms)); $("time").value = current;
  $("clock").textContent = `${seconds(current)} / ${seconds(config.duration_ms)} с`;
  const outage = config.domains.some(d => d.outages.some(o => o.start_ms<=current&&current<o.end_ms));
  const collectorOut = config.collector_outages.some(o => o.start_ms<=current&&current<o.end_ms);
  $("phase").textContent = outage ? "Общий сегмент отключён" : collectorOut ? "Накопление перед восстановлением сервера" : current>=config.generate_until_ms ? "Завершение доставки и сроков" : "Сеть доступна";
  $("phase").classList.toggle("outage",outage);
  const key = `${Math.floor(current/200)}:${leftPolicy}:${rightPolicy}:${requestNumber}`;
  const full = key !== lastFrameKey;
  renderPanel("left",leftPolicy,full); renderPanel("right",rightPolicy,full);
  if (full) { chart(); inspect(); logs(); lastFrameKey=key; }
}

function download(name, content, type) {
  const url = URL.createObjectURL(new Blob([content],{type})); const a = document.createElement("a"); a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
}

function animate(now) {
  if (playing && report) { current += Math.min(100,now-lastTick)*Number($("speed").value); if(current>=report.config.duration_ms){current=report.config.duration_ms;stop()} render(); }
  lastTick=now;requestAnimationFrame(animate);
}

$("settings").addEventListener("submit",e=>{e.preventDefault();build()});
$("play").addEventListener("click",()=>{if(!report)return;if(playing){stop()}else{if(current>=report.config.duration_ms)current=0;playing=true;$("play").textContent="Ⅱ Пауза";lastTick=performance.now()}});
$("reset").addEventListener("click",()=>{stop();current=0;render()});
$("step").addEventListener("click",()=>{stop();current+=1000;render()});
$("jump").addEventListener("click",()=>{stop();const starts=report?.config.domains.flatMap(d=>d.outages.map(o=>o.start_ms))||[];current=starts.length?Math.min(...starts):8000;render()});
$("time").addEventListener("input",()=>{stop();current=Number($("time").value);render()});
$("verbose").addEventListener("change",()=>{if(report)logs()});
for (const id of ["left","right"]) {
  const select=$(id).querySelector("select");
  select.innerHTML=Object.entries(labels).map(([value,text])=>`<option value="${value}">${esc(text)}</option>`).join("");
  select.value=id==="left"?leftPolicy:rightPolicy;
  select.addEventListener("change",()=>{if(id==="left")leftPolicy=select.value;else rightPolicy=select.value;lastFrameKey="";if(report){render();renderSummary()}});
}
$("exportJSON").addEventListener("click",()=>{if(report)download(`relay-${report.config.name}-${report.config.seed}.json`,JSON.stringify(report),"application/json")});
$("exportCSV").addEventListener("click",()=>{
  if(!report)return;const rows=[["policy","generated","delivered","expired","rejected","pending","delivery_rate","data_bytes","control_bytes","p95_ms"]];
  for(const r of report.results){const s=r.stats;rows.push([r.policy,s.generated,s.delivered,s.expired,s.rejected,s.pending,s.delivery_rate,s.data_bytes,s.control_bytes,s.p95_ms])}
  download(`relay-${report.config.name}-${report.config.seed}.csv`,rows.map(r=>r.join(",")).join("\n")+"\n","text/csv");
});
$("import").addEventListener("change",async e=>{
  const file=e.target.files[0];if(!file)return;
  try{if(file.size>20*1024*1024)throw new Error("Размер журнала больше 20 МиБ");stop();requestNumber++;hydrate(JSON.parse(await file.text()));showError("")}
  catch(error){showError("Не удалось открыть журнал: "+error.message)}
  finally{e.target.value="";$("build").disabled=!!window.RELAY_OFFLINE;$("play").disabled=!report;$("build").innerHTML=window.RELAY_OFFLINE?"Параметры сохранённого опыта":"Пересчитать опыт <span>↗</span>"}
});

async function init() {
  try{
    if(window.RELAY_OFFLINE){for(const field of $("settings").querySelectorAll("input,select"))field.disabled=true;$("build").disabled=true;$("build").textContent="Параметры сохранённого опыта"}
    const response=await fetch("/api/presets");if(!response.ok)throw new Error("Нет соединения с Go-сервером");presets=await response.json();
    $("presetCount").textContent=`${String(presets.length).padStart(2,"0")} воспроизводимых сценариев`;
    for(const [i,p] of presets.entries()){const b=document.createElement("button");b.className="preset";b.dataset.name=p.name;b.innerHTML=`<span class="preset-number">ОПЫТ ${String(i+1).padStart(2,"0")}</span>${esc(p.title)}`;b.addEventListener("click",()=>pickPreset(p.name));$("presets").append(b)}
    if(!presets.length)throw new Error("Список сценариев пуст");
    pickPreset(presets[0].name);
  }catch(error){showError(error.message)}
}
init();requestAnimationFrame(animate);
