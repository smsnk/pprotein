package analysis

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// RenderMarkdown は Report をエージェントの文脈にそのまま貼れる Markdown にする。
func RenderMarkdown(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "# analysis %s\n\n", rep.GroupId)
	fmt.Fprintf(w, "- 0s = 収集開始 %s / 区間幅 %ds\n", rep.Origin.Format("15:04:05"), rep.Bucket)
	for _, warn := range rep.Warnings {
		fmt.Fprintf(w, "- **警告**: %s\n", warn)
	}
	fmt.Fprintln(w)

	renderTimeline(w, &rep.Timeline)
	for i := range rep.Lifecycles {
		renderLifecycle(w, &rep.Lifecycles[i])
	}
	for i := range rep.Breakdowns {
		renderBreakdown(w, &rep.Breakdowns[i])
	}
}

func renderTimeline(w io.Writer, tl *Timeline) {
	if len(tl.Buckets) == 0 {
		fmt.Fprint(w, "## 時系列\n\nデータなし\n\n")
		return
	}

	fmt.Fprint(w, "## 時系列\n\n")
	head := []string{"t", "req", "2xx", "4xx", "5xx", "499", "p50", "p99"}
	head = append(head, tl.Series...)
	writeRow(w, head)
	writeSep(w, len(head))
	for _, b := range tl.Buckets {
		row := []string{
			fmt.Sprintf("%ds", b.Offset),
			itoa(b.Requests), itoa(b.S2xx), itoa(b.S4xx), itoa(b.S5xx), itoa(b.Canceled),
			fmt.Sprintf("%.3f", b.P50), fmt.Sprintf("%.3f", b.P99),
		}
		for _, s := range tl.Series {
			if v, ok := b.Series[s]; ok {
				row = append(row, num(v))
			} else {
				row = append(row, "")
			}
		}
		writeRow(w, row)
	}
	fmt.Fprintln(w)

	if len(tl.Endpoints) == 0 {
		return
	}
	fmt.Fprint(w, "## エンドポイント別の件数（区間ごと）\n\n")
	for i, e := range tl.Endpoints {
		fmt.Fprintf(w, "- E%d: `%s`\n", i+1, e)
	}
	fmt.Fprintln(w)
	head = []string{"t"}
	for i := range tl.Endpoints {
		head = append(head, fmt.Sprintf("E%d", i+1))
	}
	writeRow(w, head)
	writeSep(w, len(head))
	for _, b := range tl.Buckets {
		row := []string{fmt.Sprintf("%ds", b.Offset)}
		for _, e := range tl.Endpoints {
			row = append(row, itoa(b.Endpoints[e]))
		}
		writeRow(w, row)
	}
	fmt.Fprintln(w)
}

func renderLifecycle(w io.Writer, l *LifecycleResult) {
	fmt.Fprintf(w, "## ライフサイクル: %s（%d エンティティ）\n\n", l.Name, l.Entities)
	if l.Error != "" {
		fmt.Fprintf(w, "**失敗**: %s\n\n", l.Error)
		return
	}
	writeRow(w, []string{"#", "状態", "到達", "前から", "ここで止まった", "1件あたり回数", "所要 p50", "p90", "p99", "max"})
	writeSep(w, 10)
	for i, s := range l.Steps {
		row := []string{
			itoa(i + 1), s.Name, itoa(s.Reached), pct(s.Rate), itoa(s.StoppedHere), fmt.Sprintf("%.1f", s.PerEntity),
		}
		if q := s.FromPrev; q != nil {
			row = append(row, sec(q.P50), sec(q.P90), sec(q.P99), sec(q.Max))
		} else {
			row = append(row, "", "", "", "")
		}
		writeRow(w, row)
	}
	fmt.Fprintln(w)
}

func renderBreakdown(w io.Writer, b *BreakdownResult) {
	fmt.Fprintf(w, "## 内訳: %s（全 %d 行）\n\n", b.Name, b.TotalRows)
	if b.Error != "" {
		fmt.Fprintf(w, "**失敗**: %s\n\n", b.Error)
		return
	}
	if len(b.Rows) == 0 {
		fmt.Fprint(w, "データなし\n\n")
		return
	}
	head := append([]string{}, b.Columns...)
	if len(b.Share) > 0 {
		head = append(head, "share")
	}
	writeRow(w, head)
	writeSep(w, len(head))
	for i, r := range b.Rows {
		row := append([]string{}, r...)
		if len(b.Share) > 0 {
			row = append(row, pct(b.Share[i]))
		}
		writeRow(w, row)
	}
	fmt.Fprintln(w)
}

func writeRow(w io.Writer, cells []string) {
	for i, c := range cells {
		cells[i] = strings.ReplaceAll(c, "|", `\|`)
	}
	fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | "))
}

func writeSep(w io.Writer, n int) {
	fmt.Fprintf(w, "|%s\n", strings.Repeat(" --- |", n))
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

func sec(v float64) string { return fmt.Sprintf("%.2fs", v) }
