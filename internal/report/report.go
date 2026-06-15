// Package report generates structured reports from agent output data.
// It supports Markdown, HTML tables, JSON summaries, and terminal TUI tables,
// with a builder pattern for composing reports incrementally.
package report

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// MetricCard — a single key-value metric with optional delta and sparkline.

// MetricCard represents one tracked metric suitable for rendering in a
// dashboard or summary section.
type MetricCard struct {
	Title  string
	Value  float64
	Unit   string
	Delta  float64   // change from previous period
	Min    float64   // period minimum
	Max    float64   // period maximum
	Spark  []float64 // small sparkline data
	Format string    // sprintf format for Value, e.g. "%.2f"
	Status MetricStatus
}

// MetricStatus indicates whether the metric is trending up/down/neutral.
type MetricStatus int

const (
	StatusNeutral MetricStatus = iota
	StatusUp
	StatusDown
	StatusWarning
)

func (ms MetricStatus) String() string {
	switch ms {
	case StatusUp:
		return "up"
	case StatusDown:
		return "down"
	case StatusWarning:
		return "warn"
	}
	return "neutral"
}

// NewMetricCard creates a card with defaults.
func NewMetricCard(title string, value float64, unit string) *MetricCard {
	return &MetricCard{
		Title:  title,
		Value:  value,
		Unit:   unit,
		Format: "%.2f",
		Status: StatusNeutral,
	}
}

// WithDelta sets the delta and auto-derives status.
func (mc *MetricCard) WithDelta(delta float64) *MetricCard {
	mc.Delta = delta
	switch {
	case delta > 0:
		mc.Status = StatusUp
	case delta < 0:
		mc.Status = StatusDown
	}
	return mc
}

// WithSpark sets the sparkline and derives min/max.
func (mc *MetricCard) WithSpark(points []float64) *MetricCard {
	mc.Spark = points
	if len(points) > 0 {
		mc.Min, mc.Max = points[0], points[0]
		for _, p := range points {
			if p < mc.Min {
				mc.Min = p
			}
			if p > mc.Max {
				mc.Max = p
			}
		}
	}
	return mc
}

// FormattedValue returns Value formatted with the card's Format string.
func (mc *MetricCard) FormattedValue() string {
	return fmt.Sprintf(mc.Format, mc.Value)
}

// DeltaString returns a human-readable delta (e.g. "+12.34").
func (mc *MetricCard) DeltaString() string {
	if mc.Delta == 0 {
		return "—"
	}
	return fmt.Sprintf("%+.2f", mc.Delta)
}

// ---------------------------------------------------------------------------
// TrendLine — a named series of (time, value) points.

// TrendPoint is a single data point on a trend line.
type TrendPoint struct {
	Time  time.Time
	Value float64
	Label string // optional label for the x-axis
}

// TrendLine is a series of points with a label and optional smoothing.
type TrendLine struct {
	Name   string
	Points []TrendPoint
	Unit   string
}

// NewTrendLine creates an empty TrendLine.
func NewTrendLine(name string) *TrendLine { return &TrendLine{Name: name} }

// AddPoint appends a point; points are kept in insertion order.
func (tl *TrendLine) AddPoint(t time.Time, v float64) {
	tl.Points = append(tl.Points, TrendPoint{Time: t, Value: v})
}

// Last returns the most recent value, or 0 if empty.
func (tl *TrendLine) Last() float64 {
	if len(tl.Points) == 0 {
		return 0
	}
	return tl.Points[len(tl.Points)-1].Value
}

// Rate computes the average change per second between first and last point.
func (tl *TrendLine) Rate() float64 {
	if len(tl.Points) < 2 {
		return 0
	}
	first := tl.Points[0]
	last := tl.Points[len(tl.Points)-1]
	dur := last.Time.Sub(first.Time).Seconds()
	if dur == 0 {
		return 0
	}
	return (last.Value - first.Value) / dur
}

// SimpleMovingAverage smooths the trend line with a window of n points.
func (tl *TrendLine) SimpleMovingAverage(n int) *TrendLine {
	if n <= 1 || len(tl.Points) < n {
		return tl
	}
	smoothed := &TrendLine{Name: tl.Name + " (SMA-" + fmt.Sprint(n) + ")", Unit: tl.Unit}
	for i := n - 1; i < len(tl.Points); i++ {
		var sum float64
		for j := i - n + 1; j <= i; j++ {
			sum += tl.Points[j].Value
		}
		smoothed.AddPoint(tl.Points[i].Time, sum/float64(n))
	}
	return smoothed
}

// ---------------------------------------------------------------------------
// Report section & builder.

// Section is a named block within a report (text, table, metrics, or trend).
type SectionKind int

const (
	SectionText SectionKind = iota
	SectionTable
	SectionMetrics
	SectionTrend
)

// Section holds one part of a report.
type Section struct {
	Kind    SectionKind
	Title   string
	Body    string        // for text sections
	Headers []string      // for tables
	Rows    [][]string    // for tables
	Cards   []*MetricCard // for metrics
	Trends  []*TrendLine  // for trends
}

// ReportBuilder incrementally constructs a report.
type ReportBuilder struct {
	Title     string
	Timestamp time.Time
	sections  []Section
}

// NewReportBuilder starts a report with a title.
func NewReportBuilder(title string) *ReportBuilder {
	return &ReportBuilder{Title: title, Timestamp: time.Now()}
}

// AddText appends a free-text section.
func (rb *ReportBuilder) AddText(title, body string) *ReportBuilder {
	rb.sections = append(rb.sections, Section{
		Kind:  SectionText,
		Title: title,
		Body:  body,
	})
	return rb
}

// AddTable appends a table section.  headers and rows are copied.
func (rb *ReportBuilder) AddTable(title string, headers []string, rows [][]string) *ReportBuilder {
	rb.sections = append(rb.sections, Section{
		Kind:    SectionTable,
		Title:   title,
		Headers: append([]string(nil), headers...),
		Rows:    copyRows(rows),
	})
	return rb
}

// AddMetrics appends a metric-card section.
func (rb *ReportBuilder) AddMetrics(title string, cards ...*MetricCard) *ReportBuilder {
	rb.sections = append(rb.sections, Section{
		Kind:  SectionMetrics,
		Title: title,
		Cards: cards,
	})
	return rb
}

// AddTrends appends a trend-line section.
func (rb *ReportBuilder) AddTrends(title string, trends ...*TrendLine) *ReportBuilder {
	rb.sections = append(rb.sections, Section{
		Kind:   SectionTrend,
		Title:  title,
		Trends: trends,
	})
	return rb
}

// Sections returns the accumulated sections (useful for custom rendering).
func (rb *ReportBuilder) Sections() []Section { return rb.sections }

func copyRows(src [][]string) [][]string {
	if src == nil {
		return nil
	}
	dst := make([][]string, len(src))
	for i, row := range src {
		dst[i] = append([]string(nil), row...)
	}
	return dst
}

// ---------------------------------------------------------------------------
// FormatReportMarkdown — renders the report as GitHub-flavoured Markdown.

// FormatReportMarkdown converts a ReportBuilder to Markdown text.
func FormatReportMarkdown(rb *ReportBuilder) string {
	var b strings.Builder
	b.WriteString("# " + rb.Title + "\n\n")
	b.WriteString(fmt.Sprintf("_Generated %s_\n\n", rb.Timestamp.Format(time.RFC1123)))

	for _, sec := range rb.sections {
		switch sec.Kind {
		case SectionText:
			b.WriteString("## " + sec.Title + "\n\n")
			b.WriteString(sec.Body + "\n\n")
		case SectionTable:
			b.WriteString("## " + sec.Title + "\n\n")
			b.WriteString(renderMarkdownTable(sec.Headers, sec.Rows))
			b.WriteString("\n")
		case SectionMetrics:
			b.WriteString("## " + sec.Title + "\n\n")
			b.WriteString(renderMarkdownMetrics(sec.Cards))
			b.WriteString("\n")
		case SectionTrend:
			b.WriteString("## " + sec.Title + "\n\n")
			b.WriteString(renderMarkdownTrends(sec.Trends))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderMarkdownTable(headers []string, rows [][]string) string {
	var b strings.Builder
	// Header
	b.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(headers)) + "\n")
	for _, row := range rows {
		padded := make([]string, len(headers))
		for i := range headers {
			if i < len(row) {
				padded[i] = row[i]
			} else {
				padded[i] = ""
			}
		}
		b.WriteString("| " + strings.Join(padded, " | ") + " |\n")
	}
	return b.String()
}

func renderMarkdownMetrics(cards []*MetricCard) string {
	var b strings.Builder
	b.WriteString("| Metric | Value | Delta | Min | Max |\n")
	b.WriteString("|--------|-------|-------|-----|-----|\n")
	for _, c := range cards {
		b.WriteString(fmt.Sprintf("| %s | %s %s | %s | %.2f | %.2f |\n",
			c.Title, c.FormattedValue(), c.Unit, c.DeltaString(), c.Min, c.Max))
	}
	return b.String()
}

func renderMarkdownTrends(trends []*TrendLine) string {
	var b strings.Builder
	for _, tl := range trends {
		b.WriteString(fmt.Sprintf("**%s** (%d points, rate=%.4f/s)\n\n", tl.Name, len(tl.Points), tl.Rate()))
		if len(tl.Points) > 0 {
			b.WriteString("```\n")
			b.WriteString(asciiSpark(tl.Points, 60, 8))
			b.WriteString("\n```\n\n")
		}
	}
	return b.String()
}

// asciiSpark renders a simple ASCII sparkline.
func asciiSpark(points []TrendPoint, width int, height int) string {
	if len(points) == 0 {
		return ""
	}
	vals := make([]float64, len(points))
	minV, maxV := points[0].Value, points[0].Value
	for i, p := range points {
		vals[i] = p.Value
		if p.Value < minV {
			minV = p.Value
		}
		if p.Value > maxV {
			maxV = p.Value
		}
	}
	if maxV == minV {
		maxV = minV + 1
	}
	// Build rows bottom-up.
	rows := make([][]rune, height)
	for r := 0; r < height; r++ {
		rows[r] = make([]rune, width)
		for c := 0; c < width; c++ {
			rows[r][c] = ' '
		}
	}
	step := float64(len(points)-1) / float64(width-1)
	for col := 0; col < width; col++ {
		idx := int(float64(col) * step)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		norm := (vals[idx] - minV) / (maxV - minV)
		row := height - 1 - int(norm*float64(height-1))
		if row < 0 {
			row = 0
		}
		if row >= height {
			row = height - 1
		}
		rows[row][col] = '#'
	}
	var b strings.Builder
	for _, line := range rows {
		b.WriteString(string(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// FormatReportHTML — renders the report as a self-contained HTML document.

// FormatReportHTML converts a ReportBuilder to an HTML string.
func FormatReportHTML(rb *ReportBuilder) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">`)
	b.WriteString(`<style>body{font-family:system-ui,sans-serif;max-width:960px;margin:auto;padding:2rem}`)
	b.WriteString(`table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:.5rem}`)
	b.WriteString(`th{background:#f5f5f5}.metric{display:flex;gap:2rem;flex-wrap:wrap}`)
	b.WriteString(`.card{background:#f9f9f9;border-radius:8px;padding:1rem;min-width:180px}`)
	b.WriteString(`.card .value{font-size:2rem;font-weight:bold}.card .delta{font-size:.9rem}`)
	b.WriteString(`.up{color:green}.down{color:red}.warn{color:orange}`)
	b.WriteString(`</style></head><body>`)

	b.WriteString("<h1>" + escapeHTML(rb.Title) + "</h1>")
	b.WriteString("<p><em>Generated " + rb.Timestamp.Format(time.RFC1123) + "</em></p>")

	for _, sec := range rb.sections {
		b.WriteString("<h2>" + escapeHTML(sec.Title) + "</h2>")
		switch sec.Kind {
		case SectionText:
			b.WriteString("<p>" + escapeHTML(sec.Body) + "</p>")
		case SectionTable:
			b.WriteString(renderHTMLTable(sec.Headers, sec.Rows))
		case SectionMetrics:
			b.WriteString(renderHTMLMetrics(sec.Cards))
		case SectionTrend:
			b.WriteString(renderHTMLTrends(sec.Trends))
		}
	}
	b.WriteString("</body></html>")
	return b.String()
}

func renderHTMLTable(headers []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("<table><thead><tr>")
	for _, h := range headers {
		b.WriteString("<th>" + escapeHTML(h) + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for _, row := range rows {
		b.WriteString("<tr>")
		for i := range headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			b.WriteString("<td>" + escapeHTML(val) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

func renderHTMLMetrics(cards []*MetricCard) string {
	var b strings.Builder
	b.WriteString(`<div class="metric">`)
	for _, c := range cards {
		cls := ""
		switch c.Status {
		case StatusUp:
			cls = "up"
		case StatusDown:
			cls = "down"
		case StatusWarning:
			cls = "warn"
		}
		b.WriteString(fmt.Sprintf(`<div class="card"><div>%s</div>`,
			escapeHTML(c.Title)))
		b.WriteString(fmt.Sprintf(`<div class="value %s">%s %s</div>`, cls,
			escapeHTML(c.FormattedValue()), escapeHTML(c.Unit)))
		b.WriteString(fmt.Sprintf(`<div class="delta %s">%s</div></div>`, cls,
			escapeHTML(c.DeltaString())))
	}
	b.WriteString("</div>")
	return b.String()
}

func renderHTMLTrends(trends []*TrendLine) string {
	var b strings.Builder
	b.WriteString("<ul>")
	for _, tl := range trends {
		b.WriteString(fmt.Sprintf("<li><strong>%s</strong>: %d points, rate=%.4f/s</li>",
			escapeHTML(tl.Name), len(tl.Points), tl.Rate()))
	}
	b.WriteString("</ul>")
	return b.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// ---------------------------------------------------------------------------
// FormatTerminalTable — renders a section as a terminal-friendly TUI table.

// FormatTerminalTable renders headers and rows as a box-drawn table using
// Unicode box-drawing characters.  Column widths are auto-sized.
func FormatTerminalTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}
	// Determine column widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	// Ensure minimum width.
	for i := range widths {
		if widths[i] < 3 {
			widths[i] = 3
		}
	}
	var b strings.Builder
	// Top border.
	b.WriteString("┌")
	for i, w := range widths {
		b.WriteString(strings.Repeat("─", w+2))
		if i < len(widths)-1 {
			b.WriteString("┬")
		}
	}
	b.WriteString("┐\n")
	// Header.
	b.WriteString("│")
	for i, h := range headers {
		b.WriteString(" " + padRight(h, widths[i]) + " │")
	}
	b.WriteString("\n")
	// Header separator.
	b.WriteString("├")
	for i, w := range widths {
		b.WriteString(strings.Repeat("─", w+2))
		if i < len(widths)-1 {
			b.WriteString("┼")
		}
	}
	b.WriteString("┤\n")
	// Rows.
	for _, row := range rows {
		b.WriteString("│")
		for i := range widths {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(" " + padRight(cell, widths[i]) + " │")
		}
		b.WriteString("\n")
	}
	// Bottom border.
	b.WriteString("└")
	for i, w := range widths {
		b.WriteString(strings.Repeat("─", w+2))
		if i < len(widths)-1 {
			b.WriteString("┴")
		}
	}
	b.WriteString("┘\n")
	return b.String()
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// ---------------------------------------------------------------------------
// JSON summary.

// JSONSummary is a compact JSON-serialisable summary of a report.
type JSONSummary struct {
	Title     string        `json:"title"`
	Generated string        `json:"generated"`
	Metrics   []JSONMetric  `json:"metrics,omitempty"`
	Tables    []JSONTable   `json:"tables,omitempty"`
	Sections  []JSONSection `json:"sections,omitempty"`
}

// JSONMetric is the JSON form of a MetricCard.
type JSONMetric struct {
	Title  string  `json:"title"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Delta  float64 `json:"delta"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Status string  `json:"status"`
}

// JSONTable is the JSON form of a table section.
type JSONTable struct {
	Title   string     `json:"title"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// JSONSection is a generic section reference.
type JSONSection struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

// FormatReportJSON builds a JSONSummary from a ReportBuilder and marshals it.
func FormatReportJSON(rb *ReportBuilder) ([]byte, error) {
	summary := JSONSummary{
		Title:     rb.Title,
		Generated: rb.Timestamp.Format(time.RFC3339),
	}
	for _, sec := range rb.sections {
		kindName := "text"
		switch sec.Kind {
		case SectionTable:
			kindName = "table"
		case SectionMetrics:
			kindName = "metrics"
		case SectionTrend:
			kindName = "trend"
		}
		summary.Sections = append(summary.Sections, JSONSection{
			Title: sec.Title,
			Kind:  kindName,
		})
		switch sec.Kind {
		case SectionMetrics:
			for _, c := range sec.Cards {
				summary.Metrics = append(summary.Metrics, JSONMetric{
					Title:  c.Title,
					Value:  c.Value,
					Unit:   c.Unit,
					Delta:  c.Delta,
					Min:    c.Min,
					Max:    c.Max,
					Status: c.Status.String(),
				})
			}
		case SectionTable:
			summary.Tables = append(summary.Tables, JSONTable{
				Title:   sec.Title,
				Headers: sec.Headers,
				Rows:    sec.Rows,
			})
		}
	}
	return json.MarshalIndent(summary, "", "  ")
}

// ---------------------------------------------------------------------------
// Helper: generate sample data for testing / demo.

// SampleReport returns a ReportBuilder pre-filled with example data.
func SampleReport() *ReportBuilder {
	rb := NewReportBuilder("Lumen Agent Report")
	rb.AddText("Overview", "This report summarises agent activity over the last 24 hours. "+
		"All metrics are within expected thresholds.")
	rb.AddMetrics("Key Metrics",
		NewMetricCard("Requests", 15243, "req").WithDelta(342).WithSpark(
			[]float64{100, 120, 115, 140, 155, 148, 160}),
		NewMetricCard("Latency p99", 245.3, "ms").WithDelta(-12.1).WithSpark(
			[]float64{280, 270, 260, 255, 250, 248, 245}),
		NewMetricCard("Error Rate", 0.42, "%").WithDelta(0.05).WithSpark(
			[]float64{0.5, 0.48, 0.45, 0.43, 0.41, 0.40, 0.42}),
	)
	rb.AddTable("Top Endpoints", []string{"Endpoint", "Count", "p99 (ms)"}, [][]string{
		{"/api/chat", "8421", "210"},
		{"/api/embed", "3200", "85"},
		{"/api/search", "1800", "150"},
		{"/api/status", "1200", "12"},
	})
	rb.AddTrends("Trends",
		func() *TrendLine {
			tl := NewTrendLine("QPS")
			now := time.Now()
			for i := 0; i < 12; i++ {
				tl.AddPoint(now.Add(-time.Duration(12-i)*5*time.Minute), float64(80+i*5))
			}
			return tl
		}(),
	)
	return rb
}

// ---------------------------------------------------------------------------
// Convenience: render all formats at once.

// RenderAll returns Markdown, HTML, and JSON representations.
func RenderAll(rb *ReportBuilder) (md, html string, js []byte, err error) {
	md = FormatReportMarkdown(rb)
	html = FormatReportHTML(rb)
	js, err = FormatReportJSON(rb)
	return
}

// ---------------------------------------------------------------------------
// Percentile helpers.

// Percentile computes the p-th percentile (0-100) from sorted values.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	k := (p / 100) * float64(len(sorted)-1)
	f := math.Floor(k)
	c := math.Ceil(k)
	if f == c {
		return sorted[int(k)]
	}
	idxF := int(f)
	idxC := int(c)
	return sorted[idxF]*(c-k) + sorted[idxC]*(k-f)
}

// SummaryStats computes min, max, mean, median, p95, p99 from unsorted
// values.
func SummaryStats(vals []float64) map[string]float64 {
	if len(vals) == 0 {
		return nil
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	return map[string]float64{
		"min":    sorted[0],
		"max":    sorted[len(sorted)-1],
		"mean":   sum / float64(len(sorted)),
		"median": Percentile(sorted, 50),
		"p95":    Percentile(sorted, 95),
		"p99":    Percentile(sorted, 99),
	}
}
