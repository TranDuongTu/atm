package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

var lowerBlocks = []rune(" ▁▂▃▄▅▆▇█")

// upperBlock is the mirror of lowerBlocks[n] with the two upper-block
// glyphs the basic Unicode set offers: coarse on purpose.
func upperBlock(n int) rune {
	switch {
	case n >= 8:
		return '█'
	case n >= 4:
		return '▀'
	case n >= 1:
		return '▔'
	}
	return ' '
}

// mergePairs sums adjacent buckets until len(v) <= cols. Every series must
// be merged identically, so it is applied to all three with the same passes.
func mergePairs(v []int) []int {
	out := make([]int, 0, (len(v)+1)/2)
	for i := 0; i < len(v); i += 2 {
		s := v[i]
		if i+1 < len(v) {
			s += v[i+1]
		}
		out = append(out, s)
	}
	return out
}

// renderFlowStrip draws the mirrored rate strip: in above a baseline, done
// and evict stacked below. It returns exactly 2*rowsEach lines of
// gutter+cols cells. Colours: in HeaderLabel, done Success, evict Warning.
func renderFlowStrip(in, done, evict []int, gutter, cols, rowsEach int, st Styles) []string {
	if rowsEach < 1 {
		rowsEach = 1
	}
	for len(in) > cols && len(in) > 1 {
		in, done, evict = mergePairs(in), mergePairs(done), mergePairs(evict)
	}
	n := len(in)
	colsPer := 1
	if n > 0 && cols/n > 1 {
		colsPer = cols / n
	}
	maxV := 1
	for i := range in {
		if in[i] > maxV {
			maxV = in[i]
		}
		if done[i]+evict[i] > maxV {
			maxV = done[i] + evict[i]
		}
	}
	eighths := func(v int) int { return (v*8*rowsEach + maxV - 1) / maxV } // ceil so any non-zero shows

	above := make([][]string, rowsEach) // index 0 = row nearest the baseline
	below := make([][]string, rowsEach)
	for r := 0; r < rowsEach; r++ {
		above[r] = make([]string, 0, n*colsPer)
		below[r] = make([]string, 0, n*colsPer)
	}
	cell := func(r rune, style lipgloss.Style) string {
		if r == ' ' {
			return " "
		}
		return style.Render(string(r))
	}
	for i := 0; i < n; i++ {
		up := eighths(in[i])
		dn, dnDone := eighths(done[i]+evict[i]), eighths(done[i])
		for r := 0; r < rowsEach; r++ {
			level := up - 8*r
			if level > 8 {
				level = 8
			}
			if level < 0 {
				level = 0
			}
			for c := 0; c < colsPer; c++ {
				above[r] = append(above[r], cell(lowerBlocks[level], st.HeaderLabel))
			}
			dlevel := dn - 8*r
			if dlevel > 8 {
				dlevel = 8
			}
			if dlevel < 0 {
				dlevel = 0
			}
			style := st.Warning
			if dnDone > 8*r { // the first eighth of this row belongs to done
				style = st.Success
			}
			for c := 0; c < colsPer; c++ {
				below[r] = append(below[r], cell(upperBlock(dlevel), style))
			}
		}
	}
	pad := strings.Repeat(" ", gutter)
	tail := strings.Repeat(" ", cols-n*colsPer)
	lines := make([]string, 0, 2*rowsEach)
	for r := rowsEach - 1; r >= 0; r-- { // top of the chart is the row farthest from the baseline
		lines = append(lines, pad+strings.Join(above[r], "")+tail)
	}
	for r := 0; r < rowsEach; r++ {
		lines = append(lines, pad+strings.Join(below[r], "")+tail)
	}
	return lines
}

const (
	momentumDepthRows     = 5 // 3 plotted rows + the library's 2 x-axis rows
	momentumStripRowsEach = 2
	momentumInnerHeight   = momentumDepthRows + 2*momentumStripRowsEach + 1 // + legend
	momentumBoxHeight     = momentumInnerHeight + 2                         // + box chrome
	momentumMinWidth      = 20
)

// depthChart builds the open-depth line the way renderActivityPulseWithYMax
// does, but keeps the chart model so the strip can read Origin and
// GraphWidth and line its columns up under the plot.
func depthChart(open []int, spec chartRangeSpec, width, height int, end time.Time, st Styles) *timeserieslinechart.Model {
	start, endDay := chartWindow(spec, end)
	maxV := 1
	for _, v := range open {
		if v > maxV {
			maxV = v
		}
	}
	chart := timeserieslinechart.New(
		width,
		height,
		timeserieslinechart.WithTimeRange(start, endDay),
		timeserieslinechart.WithYRange(0, float64(maxV)),
		timeserieslinechart.WithXYSteps(4, 2),
		timeserieslinechart.WithXLabelFormatter(relXLabelFormatter(end)),
		timeserieslinechart.WithAxesStyles(st.Muted, st.Muted),
		timeserieslinechart.WithStyle(st.HeaderLabel),
	)
	for i, v := range open {
		chart.Push(timeserieslinechart.TimePoint{Time: start.AddDate(0, 0, i*spec.bucketDays), Value: float64(v)})
	}
	chart.DrawBraille()
	return &chart
}

func renderMomentumLegend(series momentumSeries, spec chartRangeSpec, width int, st Styles) string {
	in, done, evict := series.totals()
	open := 0
	if n := len(series.Open); n > 0 {
		open = series.Open[n-1]
	}
	label := spec.label
	if label == "" {
		label = spec.key
	}
	text := fmt.Sprintf("in +%d  done ✓%d  evict ✗%d  open %d   Range: %s  [Ctrl+↑/↓]", in, done, evict, open, label)
	return st.HeaderLabel.Render(fitLine(text, width))
}

// renderMomentumChart composes the depth tier, the rate strip aligned under
// its plot area, and the legend. Exactly momentumInnerHeight lines.
func renderMomentumChart(series momentumSeries, spec chartRangeSpec, width int, end time.Time, st Styles) string {
	if width < momentumMinWidth || len(series.Open) == 0 {
		return ""
	}
	chart := depthChart(series.Open, spec, width, momentumDepthRows, end, st)
	lines := strings.Split(strings.TrimRight(chart.View(), "\n"), "\n")
	for len(lines) < momentumDepthRows {
		lines = append(lines, "")
	}
	lines = lines[:momentumDepthRows]
	gutter := chart.Origin().X + 1
	cols := chart.GraphWidth()
	if cols < 1 {
		cols = 1
	}
	lines = append(lines, renderFlowStrip(series.In, series.Done, series.Evict, gutter, cols, momentumStripRowsEach, st)...)
	lines = append(lines, renderMomentumLegend(series, spec, width, st))
	return strings.Join(lines, "\n")
}
