package summary

import (
	"fmt"
	"sort"
	"strings"
)

// Markdown は Group を Markdown にする。
// そのままエージェントの文脈に貼れることを狙って、表と最小限の見出しだけで書く。
func (g *Group) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# group %s\n\n", g.ID)
	if !g.Datetime.IsZero() {
		fmt.Fprintf(&b, "- collected: %s\n", g.Datetime.Format("2006-01-02 15:04:05"))
	}
	if g.Repository != nil {
		fmt.Fprintf(&b, "- commit: %s %q (%s)\n",
			shortHash(g.Repository.Hash), firstLine(g.Repository.Message), g.Repository.Ref)
	}
	if g.Score != nil {
		fmt.Fprintf(&b, "- score: %s\n", g.Score.Summary())
	}
	if len(g.Pending) > 0 {
		fmt.Fprintf(&b, "- **収集中**: %s\n", strings.Join(g.Pending, ", "))
	}
	if len(g.Failed) > 0 {
		fmt.Fprintf(&b, "- **失敗**: %s\n", strings.Join(g.Failed, ", "))
	}
	b.WriteString("\n")

	for _, s := range g.HTTPLog {
		fmt.Fprintf(&b, "## httplog: %s (top %d by SUM)\n\n", s.Label, len(s.Endpoints))
		b.WriteString("| COUNT | METHOD | URI | SUM | AVG | P99 |\n")
		b.WriteString("| ---: | --- | --- | ---: | ---: | ---: |\n")
		for _, e := range s.Endpoints {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				num(e.Count), e.Method, e.Uri, num(e.Sum), num(e.Avg), num(e.P99))
		}
		b.WriteString("\n")
	}

	for _, s := range g.SlowLog {
		fmt.Fprintf(&b, "## slowlog: %s (top %d by total query time)\n\n", s.Label, len(s.Queries))
		b.WriteString("| COUNT | SUM | AVG | ROWS EXAMINED (avg) | QUERY |\n")
		b.WriteString("| ---: | ---: | ---: | ---: | --- |\n")
		for _, q := range s.Queries {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				num(q.Count), num(q.Sum), num(q.Avg), num(q.RowsExamined), oneLine(q.Query))
		}
		b.WriteString("\n")
	}

	for _, s := range g.Profiles {
		fmt.Fprintf(&b, "## pprof: %s (top %d by flat)\n\n", s.Label, len(s.Funcs))
		b.WriteString("| FLAT | FLAT% | CUM | FUNCTION |\n")
		b.WriteString("| ---: | ---: | ---: | --- |\n")
		for _, f := range s.Funcs {
			fmt.Fprintf(&b, "| %s | %.1f%% | %s | %s |\n",
				value(f.Flat, f.Unit), f.FlatPct, value(f.Cum, f.Unit), f.Name)
		}
		b.WriteString("\n")
	}

	for _, s := range g.Resource {
		fmt.Fprintf(&b, "## resource: %s\n\n```\n%s```\n\n", s.Label, ensureNewline(s.Text))
	}

	for _, s := range g.Memo {
		fmt.Fprintf(&b, "## memo: %s\n\n%s\n\n", s.Label, strings.TrimSpace(s.Text))
	}

	return b.String()
}

// Diff は2つの Group の差分。
type Diff struct {
	Before, After *Group
	Score         *ScoreDiff
	Endpoints     []EndpointDiff
	Queries       []QueryDiff
}

type ScoreDiff struct {
	Before, After int64
}

type EndpointDiff struct {
	Label      string
	Key        string
	CountDelta float64
	SumBefore  float64
	SumAfter   float64
}

type QueryDiff struct {
	Label     string
	Query     string
	SumBefore float64
	SumAfter  float64
}

func (d EndpointDiff) SumDelta() float64 { return d.SumAfter - d.SumBefore }
func (d QueryDiff) SumDelta() float64    { return d.SumAfter - d.SumBefore }

// Compare は before -> after のエンドポイント / クエリ単位の増減を出す。
// 片方にしか無いものも「新規 / 消滅」として残す。
func Compare(before, after *Group, topN int) *Diff {
	d := &Diff{Before: before, After: after}

	if before.Score != nil && after.Score != nil {
		d.Score = &ScoreDiff{Before: before.Score.Score, After: after.Score.Score}
	}

	for _, a := range after.HTTPLog {
		bmap := map[string]Endpoint{}
		for _, b := range before.HTTPLog {
			if b.Label != a.Label {
				continue
			}
			for _, e := range b.Endpoints {
				bmap[e.Key()] = e
			}
		}
		seen := map[string]bool{}
		for _, e := range a.Endpoints {
			prev := bmap[e.Key()]
			seen[e.Key()] = true
			d.Endpoints = append(d.Endpoints, EndpointDiff{
				Label: a.Label, Key: e.Key(),
				CountDelta: e.Count - prev.Count,
				SumBefore:  prev.Sum, SumAfter: e.Sum,
			})
		}
		for key, e := range bmap {
			if !seen[key] {
				d.Endpoints = append(d.Endpoints, EndpointDiff{
					Label: a.Label, Key: key, CountDelta: -e.Count, SumBefore: e.Sum,
				})
			}
		}
	}
	sortByAbsDelta(d.Endpoints, func(e EndpointDiff) float64 { return e.SumDelta() })
	d.Endpoints = head(d.Endpoints, topN)

	for _, a := range after.SlowLog {
		bmap := map[string]Query{}
		for _, b := range before.SlowLog {
			if b.Label != a.Label {
				continue
			}
			for _, q := range b.Queries {
				bmap[q.Key()] = q
			}
		}
		seen := map[string]bool{}
		for _, q := range a.Queries {
			prev := bmap[q.Key()]
			seen[q.Key()] = true
			d.Queries = append(d.Queries, QueryDiff{
				Label: a.Label, Query: q.Query, SumBefore: prev.Sum, SumAfter: q.Sum,
			})
		}
		for key, q := range bmap {
			if !seen[key] {
				d.Queries = append(d.Queries, QueryDiff{Label: a.Label, Query: key, SumBefore: q.Sum})
			}
		}
	}
	sortByAbsDelta(d.Queries, func(q QueryDiff) float64 { return q.SumDelta() })
	d.Queries = head(d.Queries, topN)

	return d
}

func (d *Diff) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# diff %s -> %s\n\n", d.Before.ID, d.After.ID)
	if d.Score != nil {
		delta := d.Score.After - d.Score.Before
		pct := ""
		if d.Score.Before != 0 {
			pct = fmt.Sprintf(", %+.1f%%", float64(delta)/float64(d.Score.Before)*100)
		}
		fmt.Fprintf(&b, "- score: %d -> %d (%+d%s)\n", d.Score.Before, d.Score.After, delta, pct)
	}
	for _, g := range []*Group{d.Before, d.After} {
		if g.Repository != nil {
			fmt.Fprintf(&b, "- %s: %s %q\n", g.ID, shortHash(g.Repository.Hash), firstLine(g.Repository.Message))
		}
	}
	b.WriteString("\n")

	if len(d.Endpoints) > 0 {
		b.WriteString("## httplog（SUM の増減が大きい順）\n\n")
		b.WriteString("| LABEL | ENDPOINT | SUM before | SUM after | DELTA | COUNT delta |\n")
		b.WriteString("| --- | --- | ---: | ---: | ---: | ---: |\n")
		for _, e := range d.Endpoints {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				e.Label, e.Key, num(e.SumBefore), num(e.SumAfter), signed(e.SumDelta()), signed(e.CountDelta))
		}
		b.WriteString("\n")
	}

	if len(d.Queries) > 0 {
		b.WriteString("## slowlog（合計クエリ時間の増減が大きい順）\n\n")
		b.WriteString("| LABEL | SUM before | SUM after | DELTA | QUERY |\n")
		b.WriteString("| --- | ---: | ---: | ---: | --- |\n")
		for _, q := range d.Queries {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				q.Label, num(q.SumBefore), num(q.SumAfter), signed(q.SumDelta()), oneLine(q.Query))
		}
		b.WriteString("\n")
	}

	if len(d.Endpoints) == 0 && len(d.Queries) == 0 {
		b.WriteString("比較できる計測がありません（label が揃っているか確認してください）\n")
	}

	return b.String()
}

func sortByAbsDelta[T any](s []T, delta func(T) float64) {
	sort.Slice(s, func(i, j int) bool { return abs(delta(s[i])) > abs(delta(s[j])) })
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func num(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v >= 1000 || v == float64(int64(v)):
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%.3f", v)
	}
}

func signed(v float64) string {
	if v > 0 {
		return "+" + num(v)
	}
	if v < 0 {
		return "-" + num(-v)
	}
	return "0"
}

// value は pprof の値を単位付きで読みやすくする。
func value(v float64, unit string) string {
	if unit == "nanoseconds" {
		switch {
		case v >= 1e9:
			return fmt.Sprintf("%.2fs", v/1e9)
		case v >= 1e6:
			return fmt.Sprintf("%.0fms", v/1e6)
		default:
			return fmt.Sprintf("%.0fus", v/1e3)
		}
	}
	return num(v)
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func firstLine(s string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
}

// oneLine は Markdown の表に入れられるように潰す。
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
