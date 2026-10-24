package summary

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/md"
	"github.com/smsnk/pprotein/internal/score"
)

// Markdown は Group を Markdown にする。
// そのままエージェントの文脈に貼れることを狙って、表と最小限の見出しだけで書く。
func (g *Group) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# summary %s\n\n", g.GroupId)
	if !g.Datetime.IsZero() {
		fmt.Fprintf(&b, "- 収集: %s\n", g.Datetime.Format("2006-01-02 15:04:05"))
	}
	if g.Score != nil {
		fmt.Fprintf(&b, "- スコア: %s\n", g.Score.Line())
	}
	if g.Commit != nil {
		fmt.Fprintf(&b, "- コミット: %s\n", g.Commit)
	}
	md.Warnings(&b, g.Warnings)
	b.WriteString("\n")

	for _, s := range g.HTTPLog {
		fmt.Fprintf(&b, "## httplog: %s（SUM の大きい順に %d 件）\n\n", s.Label, len(s.Endpoints))
		md.Row(&b, []string{"COUNT", "METHOD", "URI", "SUM", "AVG", "P99"})
		md.Sep(&b, 6)
		for _, e := range s.Endpoints {
			md.Row(&b, []string{md.Num(e.Count), e.Method, e.Uri, md.Sec(e.Sum), md.Sec(e.Avg), md.Sec(e.P99)})
		}
		b.WriteString("\n")
	}

	for _, s := range g.SlowLog {
		fmt.Fprintf(&b, "## slowlog: %s（合計時間の大きい順に %d 件）\n\n", s.Label, len(s.Queries))
		md.Row(&b, []string{"COUNT", "SUM", "AVG", "ROWS EXAMINED (avg)", "QUERY"})
		md.Sep(&b, 5)
		for _, q := range s.Queries {
			md.Row(&b, []string{md.Num(q.Count), md.Sec(q.Sum), md.Sec(q.Avg), md.Num(q.RowsExamined), query(q.Query)})
		}
		b.WriteString("\n")
	}

	for _, s := range g.Profiles {
		fmt.Fprintf(&b, "## pprof: %s（flat の大きい順に %d 件）\n\n", s.Label, len(s.Funcs))
		md.Row(&b, []string{"FLAT", "FLAT%", "CUM", "FUNCTION"})
		md.Sep(&b, 4)
		for _, f := range s.Funcs {
			md.Row(&b, []string{value(f.Flat, f.Unit), md.Pct(f.FlatShare), value(f.Cum, f.Unit), f.Name})
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
	Score         *score.Diff `json:",omitempty"`
	Commit        *CommitDiff `json:",omitempty"`
	// Warnings は比較の前提が崩れているもの（片方の収集中・失敗のエントリなど）。
	Warnings  []string `json:",omitempty"`
	Endpoints []EndpointDiff
	Queries   []QueryDiff
}

// CommitDiff は before / after のコミット。分からなければ nil。
type CommitDiff struct {
	Before *git.Commit `json:",omitempty"`
	After  *git.Commit `json:",omitempty"`
}

// RowDiff は before / after の同じ行（同じ label・同じキー）の組。
// 片方にしか無い行（追加・削除）は、もう片方が nil。
type RowDiff[T any] struct {
	Label string
	// Key は JSON に出さない（Before / After が持っている。SQL を何度も繰り返さない）
	Key    string `json:"-"`
	Before *T     `json:",omitempty"`
	After  *T     `json:",omitempty"`
	// Impact は件数を揃えたときの合計時間の増減。impact を参照。
	Impact float64 `json:"ImpactSec"`
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
		// analysis と同じく秒はミリ秒に丸める（JSON に浮動小数の誤差を出さない）。+ 0 で -0 を 0 にする
		out[i].Impact = math.Round(impact(out[i].Before, out[i].After)*1e3)/1e3 + 0
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
	d := &Diff{Before: before.GroupId, After: after.GroupId}
	if before.Score != nil || after.Score != nil {
		d.Score = &score.Diff{Before: before.Score, After: after.Score}
	}
	if before.Commit != nil || after.Commit != nil {
		d.Commit = &CommitDiff{Before: before.Commit, After: after.Commit}
	}
	for _, w := range before.Warnings {
		d.Warnings = append(d.Warnings, "before: "+w)
	}
	for _, w := range after.Warnings {
		d.Warnings = append(d.Warnings, "after: "+w)
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

	fmt.Fprintf(&b, "# summary diff %s -> %s\n\n", d.Before, d.After)
	if d.Score != nil {
		fmt.Fprintf(&b, "- スコア: %s\n", d.Score)
	}
	if c := d.Commit; c != nil {
		fmt.Fprintf(&b, "- コミット: %s -> %s\n", c.Before, c.After)
	}
	md.Warnings(&b, d.Warnings)
	b.WriteString("\n")

	if len(d.Endpoints) > 0 {
		b.WriteString("## httplog（IMPACT の大きい順）\n\n")
		md.Row(&b, []string{"LABEL", "ENDPOINT", "COUNT", "AVG", "SUM", "IMPACT"})
		md.Sep(&b, 6)
		for _, e := range d.Endpoints {
			count, avg, sum := arrows(e)
			md.Row(&b, []string{e.Label, e.Key, count, avg, sum, impactCell(e)})
		}
		b.WriteString("\n")
	}

	if len(d.Queries) > 0 {
		b.WriteString("## slowlog（IMPACT の大きい順）\n\n")
		md.Row(&b, []string{"LABEL", "COUNT", "AVG", "SUM", "IMPACT", "QUERY"})
		md.Sep(&b, 6)
		for _, q := range d.Queries {
			count, avg, sum := arrows(q)
			md.Row(&b, []string{q.Label, count, avg, sum, impactCell(q), query(q.Key)})
		}
		b.WriteString("\n")
	}

	if len(d.Endpoints) == 0 && len(d.Queries) == 0 {
		b.WriteString("比べられる httplog / slowlog がありません\n")
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
		return [3]string{md.Num(c), md.Sec(a), md.Sec(s)}
	}
	b, a := cells(d.Before), cells(d.After)
	return b[0] + " → " + a[0], b[1] + " → " + a[1], b[2] + " → " + a[2]
}

func impactCell[T any](d RowDiff[T]) string {
	cell := md.Signed(d.Impact, md.Sec)
	switch {
	case d.Before == nil:
		return cell + "（追加）"
	case d.After == nil:
		return cell + "（削除）"
	}
	return cell
}

// value は pprof の値を単位付きで読みやすくする。
func value(v float64, unit string) string {
	if unit != "seconds" {
		return md.Num(v)
	}
	switch {
	case v >= 1:
		return fmt.Sprintf("%.2fs", v)
	case v >= 1e-3:
		return fmt.Sprintf("%.0fms", v*1e3)
	default:
		return fmt.Sprintf("%.0fus", v*1e6)
	}
}

// query は SQL を表の1セルに収まるように1行に潰して切り詰める。
func query(s string) string {
	return md.Clip(strings.Join(strings.Fields(s), " "), 160)
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
