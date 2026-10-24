package analysis

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/md"
	"github.com/smsnk/pprotein/internal/score"
)

// 2つの走行の Report を比べる。
//
// 施策の前後で「得点の積み上がり方」「利用者がどこまで進むか」「アクセスの構成」がどう変わったかを
// 1つの表で読めるようにする。系列・ライフサイクル・内訳の中身は回ごとの設定で決まるので、
// ここでは名前で突き合わせるだけで、何の系列かは知らない。
//
// 2走行の比較では走行間のばらつきが分からないので、有意かどうかは判定しない。

type (
	Diff struct {
		Before string
		After  string
		Score  *score.Diff `json:",omitempty"`
		Commit *git.Diff   `json:",omitempty"`
		// Warnings は比較の前提が崩れているもの（設定の食い違い、片方の警告など）。
		Warnings []string `json:",omitempty"`

		Series     []SeriesDiff
		Requests   []MetricDiff
		Endpoints  []EndpointDiff
		Lifecycles []LifecycleDiff
		Breakdowns []BreakdownDiff
	}

	// SeriesDiff は系列1本。片方にしか無い系列は Before / After の片方が nil。
	SeriesDiff struct {
		Name      string
		Role      string   `json:",omitempty"`
		Before    *float64 `json:",omitempty"`
		After     *float64 `json:",omitempty"`
		T50Before *int     `json:"T50SecBefore,omitempty"`
		T50After  *int     `json:"T50SecAfter,omitempty"`
		T90Before *int     `json:"T90SecBefore,omitempty"`
		T90After  *int     `json:"T90SecAfter,omitempty"`
	}

	MetricDiff struct {
		Name   string
		Before float64
		After  float64
	}

	EndpointDiff struct {
		Key         string
		CountBefore int
		CountAfter  int
		ShareBefore float64
		ShareAfter  float64
	}

	LifecycleDiff struct {
		Name           string
		EntitiesBefore *int `json:",omitempty"`
		EntitiesAfter  *int `json:",omitempty"`
		Steps          []StepDiff
	}

	StepDiff struct {
		Name   string
		Before *StepResult `json:",omitempty"`
		After  *StepResult `json:",omitempty"`
	}

	BreakdownDiff struct {
		Name string
		// Keyed は key / value 列があって行ごとに突き合わせられたか。false なら行数だけ比べる。
		Keyed      bool
		RowsBefore *int `json:",omitempty"`
		RowsAfter  *int `json:",omitempty"`
		// Top1 / Top3 は上位1件・3件の構成比（偏りの変化を見る）。
		Top1Before float64
		Top1After  float64
		Top3Before float64
		Top3After  float64
		// NewInTop は after で上位に入り、before の上位に無かった key。
		NewInTop []string `json:",omitempty"`
		Rows     []KeyDiff
	}

	KeyDiff struct {
		Key    string
		Before *float64 `json:",omitempty"`
		After  *float64 `json:",omitempty"`
	}
)

// Compare は before → after の差を出す。top はエンドポイント構成比の行数の上限（0 なら全部）。
func Compare(before, after *Report, top int) *Diff {
	d := &Diff{Before: before.GroupId, After: after.GroupId}
	if before.Score != nil || after.Score != nil {
		d.Score = &score.Diff{Before: before.Score, After: after.Score}
	}
	if before.Commit != nil || after.Commit != nil {
		d.Commit = &git.Diff{Before: before.Commit, After: after.Commit}
	}

	if a, b := before.ConfigHash.Analysis, after.ConfigHash.Analysis; a != b {
		d.Warnings = append(d.Warnings, fmt.Sprintf("analysis.yml が走行間で違う（%s → %s）。系列・ライフサイクルの定義が変わっていないか確かめる", orDash(a), orDash(b)))
	}
	if a, b := before.ConfigHash.DBQuery, after.ConfigHash.DBQuery; a != b {
		d.Warnings = append(d.Warnings, fmt.Sprintf("dbquery.yml が走行間で違う（%s → %s）。同じ名前の SQL でも中身が変わっていると比べられない", orDash(a), orDash(b)))
	}
	for _, w := range before.Warnings {
		d.Warnings = append(d.Warnings, "before: "+w)
	}
	for _, w := range after.Warnings {
		d.Warnings = append(d.Warnings, "after: "+w)
	}

	d.Series = compareSeries(before.SeriesStats, after.SeriesStats)
	d.Requests = compareTotals(before.Totals, after.Totals)
	d.Endpoints = compareEndpoints(before.Totals.Endpoints, after.Totals.Endpoints, top)
	d.Lifecycles = compareLifecycles(before.Lifecycles, after.Lifecycles)
	d.Breakdowns = compareBreakdowns(before.Breakdowns, after.Breakdowns)
	return d
}

func compareSeries(before, after []SeriesStat) []SeriesDiff {
	idx := map[string]*SeriesDiff{}
	order := []string{}
	get := func(name string) *SeriesDiff {
		if sd, ok := idx[name]; ok {
			return sd
		}
		sd := &SeriesDiff{Name: name}
		idx[name] = sd
		order = append(order, name)
		return sd
	}
	for _, s := range before {
		sd := get(s.Name)
		v := s.Total
		sd.Before, sd.T50Before, sd.T90Before = &v, s.T50, s.T90
		sd.Role = s.Role
	}
	for _, s := range after {
		sd := get(s.Name)
		v := s.Total
		sd.After, sd.T50After, sd.T90After = &v, s.T50, s.T90
		if s.Role != "" {
			sd.Role = s.Role
		}
	}
	out := make([]SeriesDiff, 0, len(order))
	for _, name := range order {
		out = append(out, *idx[name])
	}
	// 両方にある系列を変化率の大きい順に、片方にしか無いものは後ろに
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].Before != nil && out[i].After != nil, out[j].Before != nil && out[j].After != nil
		if bi != bj {
			return bi
		}
		if !bi {
			return false
		}
		return relChange(*out[i].Before, *out[i].After) > relChange(*out[j].Before, *out[j].After)
	})
	return out
}

func compareTotals(b, a Totals) []MetricDiff {
	return []MetricDiff{
		{"requests", float64(b.Requests), float64(a.Requests)},
		{"2xx", float64(b.S2xx), float64(a.S2xx)},
		{"4xx", float64(b.S4xx), float64(a.S4xx)},
		{"5xx", float64(b.S5xx), float64(a.S5xx)},
		{"499", float64(b.Canceled), float64(a.Canceled)},
		{"p50", b.P50, a.P50},
		{"p99", b.P99, a.P99},
		{"peak req/区間", float64(b.PeakRequests), float64(a.PeakRequests)},
	}
}

func compareEndpoints(before, after map[string]int, top int) []EndpointDiff {
	tb, ta := sumCounts(before), sumCounts(after)
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	out := []EndpointDiff{}
	for k := range keys {
		e := EndpointDiff{Key: k, CountBefore: before[k], CountAfter: after[k]}
		// ほかの割合と同じく 0.01% の単位に丸める
		if tb > 0 {
			e.ShareBefore = math.Round(float64(e.CountBefore)/float64(tb)*1e4) / 1e4
		}
		if ta > 0 {
			e.ShareAfter = math.Round(float64(e.CountAfter)/float64(ta)*1e4) / 1e4
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		di := math.Abs(out[i].ShareAfter - out[i].ShareBefore)
		dj := math.Abs(out[j].ShareAfter - out[j].ShareBefore)
		if di != dj {
			return di > dj
		}
		return out[i].Key < out[j].Key
	})
	if top > 0 && len(out) > top {
		out = out[:top]
	}
	return out
}

func compareLifecycles(before, after []LifecycleResult) []LifecycleDiff {
	idx := map[string]*LifecycleDiff{}
	order := []string{}
	get := func(name string) *LifecycleDiff {
		if ld, ok := idx[name]; ok {
			return ld
		}
		ld := &LifecycleDiff{Name: name}
		idx[name] = ld
		order = append(order, name)
		return ld
	}
	steps := func(ld *LifecycleDiff, name string) *StepDiff {
		for i := range ld.Steps {
			if ld.Steps[i].Name == name {
				return &ld.Steps[i]
			}
		}
		ld.Steps = append(ld.Steps, StepDiff{Name: name})
		return &ld.Steps[len(ld.Steps)-1]
	}
	for _, l := range before {
		ld := get(l.Name)
		n := l.Entities
		ld.EntitiesBefore = &n
		for i := range l.Steps {
			s := l.Steps[i]
			steps(ld, s.Name).Before = &s
		}
	}
	for _, l := range after {
		ld := get(l.Name)
		n := l.Entities
		ld.EntitiesAfter = &n
		for i := range l.Steps {
			s := l.Steps[i]
			steps(ld, s.Name).After = &s
		}
	}
	out := []LifecycleDiff{}
	for _, name := range order {
		out = append(out, *idx[name])
	}
	return out
}

func compareBreakdowns(before, after []BreakdownResult) []BreakdownDiff {
	idx := map[string]int{}
	out := []BreakdownDiff{}
	get := func(name string) *BreakdownDiff {
		if i, ok := idx[name]; ok {
			return &out[i]
		}
		idx[name] = len(out)
		out = append(out, BreakdownDiff{Name: name})
		return &out[len(out)-1]
	}

	type side struct {
		res  *BreakdownResult
		vals map[string]float64
		keys []string
	}
	parse := func(b *BreakdownResult) *side {
		s := &side{res: b, vals: map[string]float64{}}
		ki, vi := colIndex(b.Columns, "key"), colIndex(b.Columns, "value")
		if b.Error != "" || ki < 0 || vi < 0 {
			return s
		}
		for _, row := range b.Rows {
			v, err := strconv.ParseFloat(row[vi], 64)
			if err != nil {
				continue
			}
			s.vals[row[ki]] = v
			s.keys = append(s.keys, row[ki])
		}
		return s
	}

	sides := map[string][2]*side{}
	for i := range before {
		b := &before[i]
		get(b.Name)
		p := sides[b.Name]
		p[0] = parse(b)
		sides[b.Name] = p
	}
	for i := range after {
		a := &after[i]
		get(a.Name)
		p := sides[a.Name]
		p[1] = parse(a)
		sides[a.Name] = p
	}

	for i := range out {
		bd := &out[i]
		p := sides[bd.Name]
		bs, as := p[0], p[1]
		if bs != nil && bs.res.Error == "" {
			n := bs.res.TotalRows
			bd.RowsBefore = &n
			bd.Top1Before, bd.Top3Before = topShare(bs.res.Share, 1), topShare(bs.res.Share, 3)
		}
		if as != nil && as.res.Error == "" {
			n := as.res.TotalRows
			bd.RowsAfter = &n
			bd.Top1After, bd.Top3After = topShare(as.res.Share, 1), topShare(as.res.Share, 3)
		}
		bd.Keyed = bs != nil && as != nil && hasKeyValue(bs.res) && hasKeyValue(as.res)
		if !bd.Keyed {
			continue
		}

		inBefore := map[string]bool{}
		for _, k := range bs.keys {
			inBefore[k] = true
		}
		for _, k := range as.keys {
			if !inBefore[k] {
				bd.NewInTop = append(bd.NewInTop, k)
			}
		}
		seen := map[string]bool{}
		for _, k := range append(append([]string{}, bs.keys...), as.keys...) {
			if seen[k] {
				continue
			}
			seen[k] = true
			kd := KeyDiff{Key: k}
			if v, ok := bs.vals[k]; ok {
				kd.Before = &v
			}
			if v, ok := as.vals[k]; ok {
				kd.After = &v
			}
			bd.Rows = append(bd.Rows, kd)
		}
		// 上位の外に出た key は値が取れない（上位 N 件しか持っていない）ので、差が分かるものを先に
		sort.SliceStable(bd.Rows, func(i, j int) bool {
			ri, rj := bd.Rows[i], bd.Rows[j]
			bi, bj := ri.Before != nil && ri.After != nil, rj.Before != nil && rj.After != nil
			if bi != bj {
				return bi
			}
			return math.Abs(val(ri.After)-val(ri.Before)) > math.Abs(val(rj.After)-val(rj.Before))
		})
	}
	return out
}

// --- 出力 ---

// RenderDiffMarkdown は Diff を Markdown にする。
func RenderDiffMarkdown(w io.Writer, d *Diff) {
	fmt.Fprintf(w, "# analyze diff %s -> %s\n\n", d.Before, d.After)
	if d.Score != nil {
		fmt.Fprintf(w, "- スコア: %s\n", d.Score)
	}
	if d.Commit != nil {
		fmt.Fprintf(w, "- コミット: %s\n", d.Commit)
	}
	fmt.Fprintln(w, "- 2走行の比較なので有意差は判定しない。走行間のばらつきの実測値と比べて読む")
	md.Warnings(w, d.Warnings)
	fmt.Fprintln(w)

	fmt.Fprint(w, "## 系列（両方にあるものは変化率の大きい順）\n\n")
	if len(d.Series) == 0 {
		fmt.Fprint(w, "系列なし\n\n")
	} else {
		md.Row(w, []string{"系列", "before", "after", "差", "%", "50%到達", "90%到達"})
		md.Sep(w, 7)
		for _, s := range d.Series {
			name := s.Name
			if s.Role != "" {
				name += " [" + s.Role + "]"
			}
			switch {
			case s.Before == nil:
				md.Row(w, []string{name, "（なし）", md.Num(*s.After), "追加", "", "", ""})
			case s.After == nil:
				md.Row(w, []string{name, md.Num(*s.Before), "（なし）", "削除", "", "", ""})
			default:
				md.Row(w, []string{
					name, md.Num(*s.Before), md.Num(*s.After), md.Signed(*s.After-*s.Before, md.Num), md.Change(*s.Before, *s.After),
					arrowSec(s.T50Before, s.T50After), arrowSec(s.T90Before, s.T90After),
				})
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprint(w, "## リクエスト\n\n")
	md.Row(w, []string{"指標", "before", "after", "差", "%"})
	md.Sep(w, 5)
	for _, m := range d.Requests {
		f := md.Num
		if m.Name == "p50" || m.Name == "p99" {
			f = md.Sec
		}
		md.Row(w, []string{m.Name, f(m.Before), f(m.After), md.Signed(m.After-m.Before, f), md.Change(m.Before, m.After)})
	}
	fmt.Fprintln(w)

	if len(d.Endpoints) > 0 {
		fmt.Fprint(w, "## エンドポイント構成比（比率の差が大きい順）\n\n")
		md.Row(w, []string{"エンドポイント", "件数 before", "件数 after", "比率 before", "比率 after", "比率の差"})
		md.Sep(w, 6)
		for _, e := range d.Endpoints {
			md.Row(w, []string{
				"`" + e.Key + "`", md.Int(e.CountBefore), md.Int(e.CountAfter),
				md.Pct(e.ShareBefore), md.Pct(e.ShareAfter), fmt.Sprintf("%+.1fpt", (e.ShareAfter-e.ShareBefore)*100),
			})
		}
		fmt.Fprintln(w)
	}

	for _, l := range d.Lifecycles {
		fmt.Fprintf(w, "## ライフサイクル: %s（エンティティ %s）\n\n", l.Name, arrowInt(l.EntitiesBefore, l.EntitiesAfter))
		md.Row(w, []string{"状態", "到達", "到達率", "ここで止まった", "1件あたり回数", "所要 p50", "所要 p90"})
		md.Sep(w, 7)
		for _, s := range l.Steps {
			md.Row(w, []string{
				s.Name,
				arrowStep(s, func(r *StepResult) string { return md.Int(r.Reached) }),
				arrowStep(s, func(r *StepResult) string { return md.Pct(r.Rate) }),
				arrowStep(s, func(r *StepResult) string { return md.Int(r.StoppedHere) }),
				arrowStep(s, func(r *StepResult) string { return fmt.Sprintf("%.1f", r.PerEntity) }),
				arrowStep(s, func(r *StepResult) string {
					return quantileCell(r.FromPrev, func(q *Quantiles) float64 { return q.P50 })
				}),
				arrowStep(s, func(r *StepResult) string {
					return quantileCell(r.FromPrev, func(q *Quantiles) float64 { return q.P90 })
				}),
			})
		}
		fmt.Fprintln(w)
	}

	for _, b := range d.Breakdowns {
		fmt.Fprintf(w, "## 内訳: %s（行数 %s）\n\n", b.Name, arrowInt(b.RowsBefore, b.RowsAfter))
		if !b.Keyed {
			fmt.Fprint(w, "key / value 列が無いので行数だけ比べる\n\n")
			continue
		}
		fmt.Fprintf(w, "- 上位1件の構成比 %s → %s、上位3件 %s → %s\n", md.Pct(b.Top1Before), md.Pct(b.Top1After), md.Pct(b.Top3Before), md.Pct(b.Top3After))
		if len(b.NewInTop) > 0 {
			fmt.Fprintf(w, "- 新たに上位に入った key: %s\n", strings.Join(b.NewInTop, ", "))
		}
		fmt.Fprintln(w)
		if len(b.Rows) == 0 {
			continue
		}
		md.Row(w, []string{"key", "before", "after", "差"})
		md.Sep(w, 4)
		for _, r := range b.Rows {
			diff := ""
			if r.Before != nil && r.After != nil {
				diff = md.Signed(*r.After-*r.Before, md.Num)
			}
			md.Row(w, []string{r.Key, optNum(r.Before), optNum(r.After), diff})
		}
		fmt.Fprintln(w)
	}
}

// --- helpers ---

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func hasKeyValue(b *BreakdownResult) bool {
	return b.Error == "" && colIndex(b.Columns, "key") >= 0 && colIndex(b.Columns, "value") >= 0
}

// topShare は上位 n 件の構成比の合計。ほかの割合と同じく 0.01% の単位に丸める（足すと誤差が出る）。
func topShare(share []float64, n int) float64 {
	var s float64
	for i := 0; i < n && i < len(share); i++ {
		s += share[i]
	}
	return math.Round(s*1e4) / 1e4
}

// relChange は並べ替え用の変化の大きさ。before が 0 なら after が 0 でない限り最大にする。
func relChange(before, after float64) float64 {
	if before == 0 {
		if after == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return math.Abs(after-before) / math.Abs(before)
}

func val(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func optNum(p *float64) string {
	if p == nil {
		return "（上位外）"
	}
	return md.Num(*p)
}

func arrowSec(b, a *int) string {
	f := func(p *int) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprintf("%ds", *p)
	}
	return f(b) + "→" + f(a)
}

func arrowInt(b, a *int) string {
	f := func(p *int) string {
		if p == nil {
			return "（なし）"
		}
		return md.Int(*p)
	}
	return f(b) + " → " + f(a)
}

func arrowStep(s StepDiff, f func(*StepResult) string) string {
	g := func(r *StepResult) string {
		if r == nil {
			return "（なし）"
		}
		return f(r)
	}
	return g(s.Before) + "→" + g(s.After)
}

func quantileCell(q *Quantiles, f func(*Quantiles) float64) string {
	if q == nil {
		return "-"
	}
	return md.Sec(f(q))
}
