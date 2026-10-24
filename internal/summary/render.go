package summary

import (
	"fmt"
	"math"
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

// Diff は2つの収集の差分。
type Diff struct {
	// Before / After は比べた収集の GroupId。
	Before, After string
	Score         *ScoreDiff `json:",omitempty"`
	Endpoints     []EndpointDiff
	Queries       []QueryDiff

	// before / after は見出しのコミットを書くために持つ。JSON に収集を丸ごと載せると巨大になるので出さない。
	before, after *Group
}

type ScoreDiff struct {
	Before, After int64
}

// RowDiff は before / after の同じ行（同じ label・同じキー）の組。
// 片方にしか無い行（追加・削除）は、もう片方が nil。
type RowDiff[T any] struct {
	Label  string
	Key    string
	Before *T `json:",omitempty"`
	After  *T `json:",omitempty"`
	// Impact は件数を揃えたときの合計時間の増減（秒）。impact を参照。
	Impact float64
}

type (
	EndpointDiff = RowDiff[Endpoint]
	QueryDiff    = RowDiff[Query]
)

// row は diff で突き合わせる alp / slp の1行。
type row interface {
	Key() string
	stats() (count, avg, sum float64)
}

// stats の avg は SUM / COUNT から出す。alp の AVG は小数3桁に丸められていて、
// 1〜5ms のエンドポイントでは IMPACT が件数 × 1ms の単位でしか動かない。
func (e Endpoint) stats() (count, avg, sum float64) {
	if e.Count == 0 {
		return 0, e.Avg, e.Sum
	}
	return e.Count, e.Sum / e.Count, e.Sum
}

// slp の AVG は小数6桁あるので、そのまま使う。
func (q Query) stats() (count, avg, sum float64) { return q.Count, q.Avg, q.Sum }

// impact は「リクエスト数を揃えたとき、合計でどれだけ時間が増減したか」。
//
// ベンチが固定時間だと、速くしても空いた時間に次のリクエストが入るので
// SUM はほとんど動かない（代わりに件数が増える）。SUM の増減で並べると
// いちばん効いた施策が下に埋もれるので、1件あたりの増減に件数を掛けて順位を付ける。
// 片方にしか無い行は、その SUM がまるごと増えた / 減ったものとして数える。
func impact[T row](before, after *T) float64 {
	switch {
	case before == nil:
		_, _, sum := (*after).stats()
		return sum
	case after == nil:
		_, _, sum := (*before).stats()
		return -sum
	}
	countBefore, avgBefore, _ := (*before).stats()
	countAfter, avgAfter, _ := (*after).stats()
	return (avgAfter - avgBefore) * (countBefore + countAfter) / 2
}

// compareRows は label とキーで行を突き合わせ、IMPACT の大きい順に topN 件を返す。
func compareRows[T row](before, after map[string][]T, topN int) []RowDiff[T] {
	out := []RowDiff[T]{}
	idx := map[[2]string]int{}
	add := func(rows map[string][]T, isAfter bool) {
		for label, rs := range rows {
			for i := range rs {
				k := [2]string{label, rs[i].Key()}
				j, ok := idx[k]
				if !ok {
					j = len(out)
					idx[k] = j
					out = append(out, RowDiff[T]{Label: label, Key: k[1]})
				}
				if isAfter {
					out[j].After = &rs[i]
				} else {
					out[j].Before = &rs[i]
				}
			}
		}
	}
	add(before, false)
	add(after, true)

	for i := range out {
		out[i].Impact = impact(out[i].Before, out[i].After)
	}
	// 同じ IMPACT なら label・キーの順にして、実行ごとに並びが変わらないようにする
	sort.Slice(out, func(i, j int) bool {
		if a, b := math.Abs(out[i].Impact), math.Abs(out[j].Impact); a != b {
			return a > b
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Key < out[j].Key
	})
	return head(out, topN)
}

// Compare は before -> after のエンドポイント / クエリ単位の増減を出す。
// 片方にしか無いもの（label ごと無いものも）は追加 / 削除として残す。
func Compare(before, after *Group, topN int) *Diff {
	d := &Diff{Before: before.ID, After: after.ID, before: before, after: after}

	if before.Score != nil && after.Score != nil {
		d.Score = &ScoreDiff{Before: before.Score.Score, After: after.Score.Score}
	}

	endpoints := func(g *Group) map[string][]Endpoint {
		m := map[string][]Endpoint{}
		for _, s := range g.HTTPLog {
			m[s.Label] = append(m[s.Label], s.Endpoints...)
		}
		return m
	}
	queries := func(g *Group) map[string][]Query {
		m := map[string][]Query{}
		for _, s := range g.SlowLog {
			m[s.Label] = append(m[s.Label], s.Queries...)
		}
		return m
	}
	d.Endpoints = compareRows(endpoints(before), endpoints(after), topN)
	d.Queries = compareRows(queries(before), queries(after), topN)
	return d
}

func (d *Diff) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# diff %s -> %s\n\n", d.Before, d.After)
	if d.Score != nil {
		delta := d.Score.After - d.Score.Before
		pct := ""
		if d.Score.Before != 0 {
			pct = fmt.Sprintf(", %+.1f%%", float64(delta)/float64(d.Score.Before)*100)
		}
		fmt.Fprintf(&b, "- score: %d -> %d (%+d%s)\n", d.Score.Before, d.Score.After, delta, pct)
	}
	for _, g := range []*Group{d.before, d.after} {
		if g != nil && g.Repository != nil {
			fmt.Fprintf(&b, "- %s: %s %q\n", g.ID, shortHash(g.Repository.Hash), firstLine(g.Repository.Message))
		}
	}
	b.WriteString("\n")

	if len(d.Endpoints) > 0 {
		b.WriteString("## httplog（IMPACT の大きい順）\n\n")
		b.WriteString("| LABEL | ENDPOINT | COUNT | AVG | SUM | IMPACT |\n")
		b.WriteString("| --- | --- | ---: | ---: | ---: | ---: |\n")
		for _, e := range d.Endpoints {
			count, avg, sum := arrows(e)
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", e.Label, e.Key, count, avg, sum, impactCell(e))
		}
		b.WriteString("\n")
	}

	if len(d.Queries) > 0 {
		b.WriteString("## slowlog（IMPACT の大きい順）\n\n")
		b.WriteString("| LABEL | COUNT | AVG | SUM | IMPACT | QUERY |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | --- |\n")
		for _, q := range d.Queries {
			count, avg, sum := arrows(q)
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", q.Label, count, avg, sum, impactCell(q), oneLine(q.Key))
		}
		b.WriteString("\n")
	}

	if len(d.Endpoints) == 0 && len(d.Queries) == 0 {
		b.WriteString("比較できる計測がありません（label が揃っているか確認してください）\n")
	}

	return b.String()
}

// arrows は COUNT / AVG / SUM の before → after を返す。片方にしか無ければ（なし）。
func arrows[T row](d RowDiff[T]) (count, avg, sum string) {
	cells := func(r *T) [3]string {
		if r == nil {
			return [3]string{"（なし）", "（なし）", "（なし）"}
		}
		c, a, s := (*r).stats()
		return [3]string{num(c), num(a), num(s)}
	}
	b, a := cells(d.Before), cells(d.After)
	return b[0] + " → " + a[0], b[1] + " → " + a[1], b[2] + " → " + a[2]
}

func impactCell[T any](d RowDiff[T]) string {
	switch {
	case d.Before == nil:
		return signed(d.Impact) + "（追加）"
	case d.After == nil:
		return signed(d.Impact) + "（削除）"
	}
	return signed(d.Impact)
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
		// バイト数で切るので、途中で切れた最後の文字は落とす
		s = strings.ToValidUTF8(s[:160], "") + "…"
	}
	return s
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
