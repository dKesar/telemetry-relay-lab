package research

import (
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dKesar/telemetry-relay-lab/internal/selector"
	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

func chart(r Report, kind string) string {
	var b strings.Builder
	cases := Cases()
	height := 140 + len(cases)*48
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="1100" height="%d" viewBox="0 0 1100 %d" role="img"><rect width="100%%" height="100%%" fill="white"/><g font-family="Arial, sans-serif" fill="#20332d">`, height, height)
	title := "Доставка: старый метод и адаптивный"
	subtitle := "Среднее по независимым seed; 95% bootstrap-интервалы. Модель, не физическая сеть."
	low, high := 0., 100.
	if kind != "delivery" {
		low, high = -10., 10.
		for _, p := range r.Comparisons {
			if p.Baseline != selector.RiskAware {
				continue
			}
			v := p.DeliveryDelta
			if kind == "cost" {
				v = p.ByteSaving
			}
			low = math.Min(low, math.Floor(v.Low*10)*10)
			high = math.Max(high, math.Ceil(v.High*10)*10)
		}
	}
	if kind == "gain" {
		title = "Разность доставки: адаптивный минус старый"
		subtitle = "Процентные пункты; 95% парные bootstrap-интервалы по seed. Нулевая линия означает отсутствие разности."
	}
	if kind == "cost" {
		title = "Экономия модельных байтов относительно старого метода"
		subtitle = "Проценты; положительное значение означает меньший расход. Интерпретировать вместе с доставкой."
	}
	subtitle += fmt.Sprintf(" n=%d seed; %s.", r.Runs, r.Model)
	fmt.Fprintf(&b, `<text x="28" y="34" font-size="23" font-weight="bold">%s</text><text x="28" y="60" font-size="13">%s</text>`, title, subtitle)
	if kind == "delivery" {
		b.WriteString(`<rect x="255" y="70" width="12" height="9" fill="#af8c76"/><text x="274" y="79" font-size="11">Старый risk_aware</text><rect x="560" y="70" width="12" height="9" fill="#247c65"/><text x="579" y="79" font-size="11">Новый adaptive</text>`)
	}
	x := func(v float64) float64 { return 255 + (v-low)/(high-low)*780 }
	for j := 0; j <= 5; j++ {
		v := low + (high-low)*float64(j)/5
		fmt.Fprintf(&b, `<path d="M %.2f 83 V %d" stroke="#e1e8e4"/><text x="%.2f" y="%d" font-size="12" text-anchor="middle">%.1f</text>`, x(v), height-43, x(v), height-20, v)
	}
	if kind != "delivery" {
		fmt.Fprintf(&b, `<path d="M %.2f 83 V %d" stroke="#657a70" stroke-width="1.5"/>`, x(0), height-43)
	}
	for i, c := range cases {
		y := 104 + 48*i
		fmt.Fprintf(&b, `<text x="26" y="%d" font-size="13" fill="#20332d" font-family="DejaVu Sans, sans-serif">%s</text>`, y+4, html.EscapeString(c.Name))
		if kind == "delivery" {
			for k, p := range []selector.Policy{selector.RiskAware, sim.Adaptive} {
				for _, s := range r.Summary {
					if s.Case != c.Name || s.Policy != p {
						continue
					}
					v := s.Delivery
					color := "#af8c76"
					if k == 1 {
						color = "#247c65"
					}
					cy := y - 7 + k*15
					fmt.Fprintf(&b, `<rect x="255" y="%d" width="%.2f" height="10" rx="2" fill="%s"/><path d="M %.2f %d H %.2f M %.2f %d v 8 M %.2f %d v 8" stroke="#20332d"/><text x="%.2f" y="%d" font-size="10">%.1f%%</text>`, cy, x(v.Mean*100)-255, color, x(v.Low*100), cy+5, x(v.High*100), x(v.Low*100), cy+1, x(v.High*100), cy+1, x(v.High*100)+5, cy+9, v.Mean*100)
				}
			}
		} else {
			for _, p := range r.Comparisons {
				if p.Case != c.Name || p.Baseline != selector.RiskAware {
					continue
				}
				v := p.DeliveryDelta
				if kind == "cost" {
					v = p.ByteSaving
				}
				color := "#60766c"
				if v.Low > 0 {
					color = "#247c65"
				}
				if v.High < 0 {
					color = "#a94442"
				}
				fmt.Fprintf(&b, `<path d="M %.2f %d H %.2f M %.2f %d v 12 M %.2f %d v 12" stroke="%s" stroke-width="2"/><circle cx="%.2f" cy="%d" r="5" fill="%s"/><text x="1045" y="%d" font-size="11">%+.1f</text>`, x(v.Low*100), y, x(v.High*100), x(v.Low*100), y-6, x(v.High*100), y-6, color, x(v.Mean*100), y, color, y+4, v.Mean*100)
			}
		}
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}

func WriteEvidence(dest string, r Report) error {
	figs := map[string]string{"delivery": chart(r, "delivery"), "gain": chart(r, "gain"), "cost": chart(r, "cost")}
	for name, svg := range figs {
		if err := os.WriteFile(filepath.Join(dest, name+".svg"), []byte(svg), 0644); err != nil {
			return err
		}
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RelayLab: проверка адаптивного алгоритма</title><style>body{margin:0;background:#f2f6f3;color:#20332d;font:16px/1.6 Arial,sans-serif}main{max-width:1180px;margin:auto;padding:32px}h1{font-size:34px;line-height:1.2}h2{margin-top:36px}article,.figure{background:white;border:1px solid #d9e3dd;border-radius:12px;padding:22px;margin:20px 0}.meta{color:#536f61}.figure{overflow:auto}.figure svg{width:100%;min-width:760px;height:auto}.scroll{overflow:auto}table{border-collapse:collapse;min-width:900px;width:100%;font-size:13px}th,td{padding:9px 10px;border-bottom:1px solid #e0e8e3;text-align:right}th:first-child,td:first-child{text-align:left}.bad{color:#a94442}.good{color:#247c65}code{background:#e8efea;padding:3px 6px}@media print{body{background:white}main{padding:0}.figure{break-inside:avoid}article{break-inside:avoid}}</style><main><p class="meta">RELAY LAB / ВОСПРОИЗВОДИМАЯ ПРОВЕРКА / МОДЕЛЬ</p><h1>Что изменилось после добавления обучения и выбора набора копий</h1>`)
	fmt.Fprintf(&b, `<p>Модель <code>%s</code>, алгоритм <code>%s</code>. %d сценариев × %d seed × %d методов = <strong>%d проверенных прогонов</strong>. Начальный seed: %d.</p>`, r.Model, r.Algorithm, len(Cases()), r.Runs, len(sim.ResearchPolicies()), r.Checks, r.Seed)
	b.WriteString(`<article><h2>Как читать результат</h2><p>Старый метод: risk_aware с фиксированными оценками. Новый: adaptive с совместной историей проверок, оценкой срока и ценой дополнительной копии. Бюджет, очереди, отказы и случайные исходы совпадают внутри каждой пары. Стратегия all использует больший бюджет и служит отдельным ориентиром.</p><p>У всех методов одинаково учтены служебные проверки. Неудачные сценарии включены. Доверительные интервалы отражают вариацию seed в этой модели; они не учитывают ошибку выбора модели реального объекта.</p><p>Проверено независимо от оптимизатора: баланс сообщений, бюджет копий, уникальность получателей и своевременность всех засчитанных доставок. Отсутствие нарушений не доказывает оптимальность.</p></article>`)
	for _, name := range []string{"delivery", "gain", "cost"} {
		b.WriteString(`<section class="figure">`)
		b.WriteString(figs[name])
		b.WriteString(`</section>`)
	}
	b.WriteString(`<article><h2>Все сравнения, включая отключение компонентов</h2><p>adaptive_independent отключает совместную историю; adaptive_no_deadline отключает учёт полного времени до сервера в функции полезности. Это абляции нашего метода, не реализации PRoPHET или RAPID. Интервалы приведены для отдельных контрастов, без одновременного покрытия всех сравнений.</p><div class="scroll"><table><thead><tr><th>Сценарий / контроль</th><th>Δ доставки, п.п.</th><th>95% интервал</th><th>Экономия байтов</th><th>Вывод</th></tr></thead><tbody>`)
	for _, p := range r.Comparisons {
		cls := ""
		if p.DeliveryDelta.Low > 0 {
			cls = "good"
		}
		if p.DeliveryDelta.High < 0 {
			cls = "bad"
		}
		fmt.Fprintf(&b, `<tr class="%s"><td>%s / %s</td><td>%+.2f</td><td>[%+.2f; %+.2f]</td><td>%+.1f%%</td><td>%s</td></tr>`, cls, html.EscapeString(p.Case), p.Baseline, p.DeliveryDelta.Mean*100, p.DeliveryDelta.Low*100, p.DeliveryDelta.High*100, p.ByteSaving.Mean*100, html.EscapeString(p.Decision))
	}
	b.WriteString(`</tbody></table></div></article><article><h2>Как воспроизвести и что сохранить</h2><p>Запустить <code>go run ./cmd/relaylab research -runs 30 -seed 1000 -out results/my-proof</code>. В новом каталоге сохранятся worlds.json (все условия опыта), runs.csv (все прогоны), summary.json, manifest.json, три SVG и этот HTML.</p><p>Сохраните SHA коммита командой <code>git rev-parse HEAD</code>. Проверка SHA-256: manifest содержит хеш компактной JSON-сериализации worlds.json, без форматирующих пробелов. Отчёт формируется программой, значения не дорисовываются вручную.</p><p>Для доклада: кадр графика доставки, кадр парных разностей с нулевой линией и кадр стоимости. На каждом оставить название модели, оси, интервалы, список сценариев и число seed. Для наглядного объяснения дополнительно показать пошаговый replay сети.</p><p>Ограничения: параметры метода фиксированы до seed 1000+, но сценарии спроектированы авторами стенда. Это проверка на отложенных seed, а не независимая внешняя валидация. Отдельный канал проб не конкурирует с данными за слоты; его байты учтены. Нет физических агентов, радиоканала и криптографической защиты.</p></article></main></html>`)
	return os.WriteFile(filepath.Join(dest, "evidence.html"), []byte(b.String()), 0644)
}
