package analysis

import (
	"fmt"
	"io"
	"strings"

	"github.com/smsnk/pprotein/internal/md"
)

// RenderMarkdown は Report をエージェントの文脈にそのまま貼れる Markdown にする。
func RenderMarkdown(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "# analyze %s\n\n", rep.GroupId)
	fmt.Fprintf(w, "- 0s = 収集開始 %s / 区間幅 %ds\n", rep.Origin.Format("15:04:05"), rep.Bucket)
	if rep.Score != nil {
		fmt.Fprintf(w, "- スコア: %s\n", rep.Score)
	}
	if c := rep.Commit; c != nil {
		fmt.Fprintf(w, "- コミット: %s\n", c)
	}
	fmt.Fprintf(w, "- 設定: analysis.yml %s / dbquery.yml %s\n", orDash(rep.ConfigHash.Analysis), orDash(rep.ConfigHash.DBQuery))
	if roles := rolesLine(rep.SeriesStats); roles != "" {
		fmt.Fprintf(w, "- 系列の役割: %s\n", roles)
	}
	md.Warnings(w, rep.Warnings)
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
	md.Row(w, head)
	md.Sep(w, len(head))
	for _, b := range tl.Buckets {
		row := []string{
			fmt.Sprintf("%ds", b.Offset),
			md.Int(b.Requests), md.Int(b.S2xx), md.Int(b.S4xx), md.Int(b.S5xx), md.Int(b.Canceled),
			md.Sec(b.P50), md.Sec(b.P99),
		}
		for _, s := range tl.Series {
			if v, ok := b.Series[s]; ok {
				row = append(row, md.Num(v))
			} else {
				row = append(row, "")
			}
		}
		md.Row(w, row)
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
	md.Row(w, head)
	md.Sep(w, len(head))
	for _, b := range tl.Buckets {
		row := []string{fmt.Sprintf("%ds", b.Offset)}
		for _, e := range tl.Endpoints {
			row = append(row, md.Int(b.Endpoints[e]))
		}
		md.Row(w, row)
	}
	fmt.Fprintln(w)
}

func renderLifecycle(w io.Writer, l *LifecycleResult) {
	fmt.Fprintf(w, "## ライフサイクル: %s（%d エンティティ）\n\n", l.Name, l.Entities)
	if l.Error != "" {
		fmt.Fprintf(w, "**失敗**: %s\n\n", l.Error)
		return
	}
	md.Row(w, []string{"#", "状態", "到達", "前から", "ここで止まった", "1件あたり回数", "所要 p50", "p90", "p99", "max"})
	md.Sep(w, 10)
	for i, s := range l.Steps {
		row := []string{
			md.Int(i + 1), s.Name, md.Int(s.Reached), md.Pct(s.Rate), md.Int(s.StoppedHere), fmt.Sprintf("%.1f", s.PerEntity),
		}
		if q := s.FromPrev; q != nil {
			row = append(row, md.Sec(q.P50), md.Sec(q.P90), md.Sec(q.P99), md.Sec(q.Max))
		} else {
			row = append(row, "", "", "", "")
		}
		md.Row(w, row)
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
	md.Row(w, head)
	md.Sep(w, len(head))
	for i, r := range b.Rows {
		row := append([]string{}, r...)
		if len(b.Share) > 0 {
			row = append(row, md.Pct(b.Share[i]))
		}
		md.Row(w, row)
	}
	fmt.Fprintln(w)
}

// rolesLine は「score: tip / driver: … / penalty: …」の形で系列の役割をまとめる。
func rolesLine(stats []SeriesStat) string {
	byRole := map[string][]string{}
	for _, s := range stats {
		if s.Role != "" {
			byRole[s.Role] = append(byRole[s.Role], s.Name)
		}
	}
	parts := []string{}
	for _, r := range []string{"score", "driver", "penalty"} {
		if len(byRole[r]) > 0 {
			parts = append(parts, r+": "+strings.Join(byRole[r], ", "))
		}
	}
	return strings.Join(parts, " / ")
}
