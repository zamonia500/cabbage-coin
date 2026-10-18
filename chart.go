package main

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strings"
)

// Chart colors are CSS variables so dark mode can restep them (see style.css).
const (
	colorFocus   = "var(--series-focus)"
	colorContext = "var(--series-context)"
)

type chartSeries struct {
	Name   string
	Values []float64 // aligned with the chart's x labels; NaN = gap
	Color  string
}

func monthLabel(ym string) string { return strings.Replace(ym, "-", ".", 1) }
func dayLabel(d string) string {
	if len(d) == 10 {
		return d[5:7] + "/" + d[8:]
	}
	return d
}

// lineChart renders a small multi-line SVG chart: one y axis, hairline grid,
// 2px lines, the last value labeled, per-point hover titles, and a table view.
func lineChart(lang string, xs []string, series []chartSeries, label func(string) string) template.HTML {
	if len(xs) < 2 {
		return template.HTML(`<p class="muted">` + html.EscapeString(msgs[lang]["no_data"]) + `</p>`)
	}
	const W, H, left, right, top, bottom = 320.0, 150.0, 40.0, 44.0, 10.0, 22.0
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for _, v := range s.Values {
			if !math.IsNaN(v) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
	}
	if math.IsInf(lo, 0) {
		return template.HTML(`<p class="muted">` + html.EscapeString(msgs[lang]["no_data"]) + `</p>`)
	}
	step := niceStep((hi - lo) / 3)
	lo, hi = math.Floor(lo/step)*step, math.Ceil(hi/step)*step
	if hi == lo {
		hi = lo + step
	}
	x := func(i int) float64 { return left + float64(i)*(W-left-right)/float64(len(xs)-1) }
	y := func(v float64) float64 { return top + (hi-v)*(H-top-bottom)/(hi-lo) }

	var b strings.Builder
	fmt.Fprintf(&b, `<figure class="chart"><svg viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, W, H, html.EscapeString(series[0].Name))
	for v := lo; v <= hi+step/2; v += step {
		fmt.Fprintf(&b, `<line class="grid" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, left, W-right, y(v), y(v))
		fmt.Fprintf(&b, `<text class="tick" x="%.1f" y="%.1f" text-anchor="end">%s</text>`, left-6, y(v)+3, comma(int(v)))
	}
	for _, i := range []int{0, len(xs) / 2, len(xs) - 1} {
		anchor := map[bool]string{true: "start", false: "middle"}[i == 0]
		if i == len(xs)-1 {
			anchor = "end"
		}
		fmt.Fprintf(&b, `<text class="tick" x="%.1f" y="%.1f" text-anchor="%s">%s</text>`, x(i), H-6, anchor, html.EscapeString(label(xs[i])))
	}
	// Context series first so the focus line draws on top.
	for k := len(series) - 1; k >= 0; k-- {
		s := series[k]
		var d strings.Builder
		pen := "M"
		for i, v := range s.Values {
			if math.IsNaN(v) {
				pen = "M"
				continue
			}
			fmt.Fprintf(&d, "%s%.1f %.1f ", pen, x(i), y(v))
			pen = "L"
		}
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>`, d.String(), s.Color)
		if last := len(s.Values) - 1; last >= 0 && !math.IsNaN(s.Values[last]) {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s" class="ring"/>`, x(last), y(s.Values[last]), s.Color)
			if k == 0 {
				fmt.Fprintf(&b, `<text class="endlabel" x="%.1f" y="%.1f">%s</text>`, x(last)+6, y(s.Values[last])+4, comma(int(math.Round(s.Values[last]))))
			}
		}
	}
	// Hover targets wider than the marks, one per x position.
	for i := range xs {
		var tip strings.Builder
		tip.WriteString(label(xs[i]))
		for _, s := range series {
			if i < len(s.Values) && !math.IsNaN(s.Values[i]) {
				fmt.Fprintf(&tip, "\n%s: ¥%s", s.Name, comma(int(math.Round(s.Values[i]))))
			}
		}
		w := (W - left - right) / float64(len(xs)-1)
		fmt.Fprintf(&b, `<rect class="hit" x="%.1f" y="%.1f" width="%.1f" height="%.1f"><title>%s</title></rect>`, x(i)-w/2, top, w, H-top-bottom, html.EscapeString(tip.String()))
	}
	b.WriteString(`</svg>`)
	if len(series) > 1 {
		b.WriteString(`<figcaption class="legend">`)
		for _, s := range series {
			fmt.Fprintf(&b, `<span><i style="background:%s"></i>%s</span>`, s.Color, html.EscapeString(s.Name))
		}
		b.WriteString(`</figcaption>`)
	}
	fmt.Fprintf(&b, `<details class="table"><summary>%s</summary><table><tr><th></th>`, html.EscapeString(msgs[lang]["table"]))
	for _, s := range series {
		fmt.Fprintf(&b, `<th>%s</th>`, html.EscapeString(s.Name))
	}
	b.WriteString(`</tr>`)
	for i := len(xs) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, `<tr><td>%s</td>`, html.EscapeString(label(xs[i])))
		for _, s := range series {
			cell := "–"
			if i < len(s.Values) && !math.IsNaN(s.Values[i]) {
				cell = "¥" + comma(int(math.Round(s.Values[i])))
			}
			fmt.Fprintf(&b, `<td>%s</td>`, cell)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table></details></figure>`)
	return template.HTML(b.String())
}

// niceStep rounds a raw tick step to 1, 2 or 5 × 10^n.
func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if raw <= m*p {
			return m * p
		}
	}
	return 10 * p
}
