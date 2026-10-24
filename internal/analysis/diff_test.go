package analysis

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/score"
)

func intp(n int) *int { return &n }

func TestTotalsAndSeriesStats(t *testing.T) {
	cfg := mustConfig(t, `
bucket: 10
series:
  - {name: tip, query: tips}
  - {name: peak, query: tips, agg: max}
`)
	in := &Input{
		Origin: origin,
		Requests: []Request{
			{T: origin - 1, Method: "GET", Endpoint: "/a", Status: 200, ReqTime: 9},
			{T: origin + 1, Method: "GET", Endpoint: "/a", Status: 200, ReqTime: 0.1},
			{T: origin + 2, Method: "GET", Endpoint: "/a", Status: 499, ReqTime: 20},
			{T: origin + 15, Method: "POST", Endpoint: "/b", Status: 500, ReqTime: 0.3},
		},
		Queries: map[string]*dbquery.QueryResult{
			"tips": {Columns: []string{"t", "value"}, Rows: rows(
				[]string{"1700000001", "40"},
				[]string{"1700000012", "50"},
				[]string{"1700000025", "10"},
			)},
		},
	}
	rep := Analyze(cfg, in)
	tot := rep.Totals
	// 収集開始より前のリクエストは数えない
	if tot.Requests != 3 || tot.Canceled != 1 || tot.S5xx != 1 || tot.Endpoints["GET /a"] != 2 {
		t.Errorf("totals = %+v", tot)
	}
	if tot.PeakRequests != 2 || tot.PeakOffset != 0 || tot.P99 != 20 {
		t.Errorf("peak / p99 = %+v", tot)
	}

	stats := map[string]SeriesStat{}
	for _, s := range rep.SeriesStats {
		stats[s.Name] = s
	}
	tip := stats["tip"]
	// 区間 0s:40, 10s:50, 20s:10 → 合計100。50% は 10s の区間の終わり(20s)、90% も 20s
	if tip.Total != 100 || tip.T50 == nil || *tip.T50 != 20 || tip.T90 == nil || *tip.T90 != 20 {
		t.Errorf("tip = %+v (T50=%v T90=%v)", tip, tip.T50, tip.T90)
	}
	if p := stats["peak"]; p.Total != 50 || p.T50 != nil {
		t.Errorf("max series = %+v", p)
	}
}

func report(id string, hash string, value int64) *Report {
	return &Report{
		GroupId:    id,
		Score:      &score.Brief{Value: value, Passed: true},
		ConfigHash: ConfigHash{Analysis: hash, DBQuery: "db1"},
	}
}

func TestCompareWarnsOnConfigMismatchAndSplitsSeries(t *testing.T) {
	b := report("A", "h1", 3000)
	b.SeriesStats = []SeriesStat{
		{Name: "tip", Agg: "sum", Total: 3000, T50: intp(50), T90: intp(85)},
		{Name: "reaction", Agg: "count", Total: 200},
		{Name: "old", Agg: "count", Total: 5},
	}
	a := report("B", "h2", 3600)
	a.SeriesStats = []SeriesStat{
		{Name: "tip", Agg: "sum", Total: 3600, T50: intp(45), T90: intp(80)},
		{Name: "reaction", Agg: "count", Total: 100},
		{Name: "new", Agg: "count", Total: 7},
	}
	d := Compare(b, a, 10)

	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "analysis.yml") {
		t.Errorf("warnings = %v", d.Warnings)
	}
	// 両方にある系列は変化率の大きい順（reaction -50% > tip +20%）、片方だけのものは後ろ
	names := []string{}
	for _, s := range d.Series {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "reaction,tip,old,new" {
		t.Errorf("series order = %s", got)
	}
	if d.Series[2].After != nil || d.Series[3].Before != nil {
		t.Errorf("one-sided series = %+v / %+v", d.Series[2], d.Series[3])
	}

	var buf bytes.Buffer
	RenderDiffMarkdown(&buf, d)
	out := buf.String()
	for _, want := range []string{
		"- スコア: 3000 -> 3600 (+600, +20.0%)",
		"| tip | 3000 | 3600 | +600 | +20.0% | 50s→45s | 85s→80s |",
		"| old | 5 | （なし） | 削除 |",
		"| new | （なし） | 7 | 追加 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q:\n%s", want, out)
		}
	}
}

// summary diff と同じく、比べた2回のコミットを出す
func TestCompareShowsCommits(t *testing.T) {
	b, a := report("A", "h1", 1), report("B", "h1", 2)
	b.Commit = &git.Commit{Hash: "aaaaaaaaaa", Message: "before"}
	a.Commit = &git.Commit{Hash: "bbbbbbbbbb", Message: "after", Ref: "refs/heads/x"}
	d := Compare(b, a, 10)
	var buf bytes.Buffer
	RenderDiffMarkdown(&buf, d)
	if !strings.Contains(buf.String(), `- コミット: aaaaaaa "before" -> bbbbbbb "after" (refs/heads/x)`) {
		t.Errorf("markdown:\n%s", buf.String())
	}
	if d.Commit == nil || d.Commit.Before.Hash != "aaaaaaaaaa" {
		t.Errorf("Commit = %+v", d.Commit)
	}
}

func TestCompareEndpointsByShare(t *testing.T) {
	b := report("A", "h", 0)
	b.Totals.Endpoints = map[string]int{"GET /icon": 80, "POST /comment": 20}
	a := report("B", "h", 0)
	a.Totals.Endpoints = map[string]int{"GET /icon": 40, "POST /comment": 40, "GET /new": 20}
	d := Compare(b, a, 2)
	if len(d.Endpoints) != 2 {
		t.Fatalf("endpoints = %+v", d.Endpoints)
	}
	// 比率の差: icon 80%→40% (-40pt), comment 20%→40% (+20pt), new 0→20% (+20pt)
	if d.Endpoints[0].Key != "GET /icon" || d.Endpoints[0].ShareAfter != 0.4 {
		t.Errorf("first = %+v", d.Endpoints[0])
	}
}

func TestCompareLifecyclesAlignsStepsByName(t *testing.T) {
	b := report("A", "h", 0)
	b.Lifecycles = []LifecycleResult{{Name: "viewer", Entities: 19, Steps: []StepResult{
		{Name: "enter", Reached: 19, Rate: 1},
		{Name: "post", Reached: 17, Rate: 17.0 / 19, FromPrev: &Quantiles{P50: 0.5, P90: 3.7}},
	}}}
	a := report("B", "h", 0)
	a.Lifecycles = []LifecycleResult{{Name: "viewer", Entities: 30, Steps: []StepResult{
		{Name: "enter", Reached: 30, Rate: 1},
		{Name: "post", Reached: 29, Rate: 29.0 / 30, FromPrev: &Quantiles{P50: 0.2, P90: 1.0}},
		{Name: "exit", Reached: 10, Rate: 10.0 / 29},
	}}}
	d := Compare(b, a, 10)
	l := d.Lifecycles[0]
	if *l.EntitiesBefore != 19 || *l.EntitiesAfter != 30 || len(l.Steps) != 3 || l.Steps[2].Before != nil {
		t.Errorf("lifecycle = %+v", l)
	}
	var buf bytes.Buffer
	RenderDiffMarkdown(&buf, d)
	if !strings.Contains(buf.String(), "| post | 17→29 | 89.5%→96.7% | 0→0 | 0.0→0.0 | 0.500s→0.200s | 3.700s→1.000s |") {
		t.Errorf("markdown:\n%s", buf.String())
	}
}

func TestCompareBreakdowns(t *testing.T) {
	kv := func(rowsIn ...[]string) BreakdownResult {
		var total float64
		res := BreakdownResult{Name: "by streamer", Columns: []string{"key", "value"}, TotalRows: len(rowsIn)}
		for _, r := range rowsIn {
			res.Rows = append(res.Rows, r)
			v, _ := strconv.ParseFloat(r[1], 64)
			total += v
		}
		for _, r := range rowsIn {
			v, _ := strconv.ParseFloat(r[1], 64)
			res.Share = append(res.Share, v/total)
		}
		return res
	}
	b := report("A", "h", 0)
	b.Breakdowns = []BreakdownResult{
		kv([]string{"alice", "600"}, []string{"bob", "300"}, []string{"carol", "100"}),
		{Name: "no key", Columns: []string{"livestream", "tips"}, Rows: [][]string{{"1", "2"}}, TotalRows: 1},
	}
	a := report("B", "h", 0)
	a.Breakdowns = []BreakdownResult{
		kv([]string{"bob", "500"}, []string{"dave", "300"}, []string{"alice", "200"}),
		{Name: "no key", Columns: []string{"livestream", "tips"}, Rows: [][]string{{"1", "2"}, {"2", "3"}}, TotalRows: 2},
	}
	d := Compare(b, a, 10)
	bd := d.Breakdowns[0]
	if !bd.Keyed || bd.Top1Before != 0.6 || bd.Top1After != 0.5 {
		t.Errorf("breakdown = %+v", bd)
	}
	if strings.Join(bd.NewInTop, ",") != "dave" {
		t.Errorf("new in top = %v", bd.NewInTop)
	}
	// 両方にある key を差の大きい順に（alice -400, bob +200）、片方だけのものは後ろで値の大きい順（dave 300, carol 100）
	keys := []string{}
	for _, r := range bd.Rows {
		keys = append(keys, r.Key)
	}
	if got := strings.Join(keys, ","); got != "alice,bob,dave,carol" {
		t.Errorf("rows = %s", got)
	}
	if nk := d.Breakdowns[1]; nk.Keyed || *nk.RowsBefore != 1 || *nk.RowsAfter != 2 {
		t.Errorf("unkeyed = %+v", nk)
	}
}

// AI がドキュメント無しで読めるように、JSON のキーに単位が付き、小数が丸まっていること。
func TestReportJSONIsSelfDescribing(t *testing.T) {
	cfg := mustConfig(t, `
httplog:
  entity_key: session
series:
  - {name: tip, query: tips, role: score}
lifecycles:
  - name: viewer
    source: httplog
    steps:
      - {name: a, match: ^GET /a$}
      - {name: b, match: ^GET /b$}
`)
	in := &Input{
		Origin: origin,
		Requests: []Request{
			{T: origin + 1, Method: "GET", Endpoint: "/a", Status: 499, ReqTime: 0.03299999237060547, Entity: "x"},
			{T: origin + 1.0329999, Method: "GET", Endpoint: "/b", Status: 200, ReqTime: 0.1, Entity: "x"},
		},
		Queries: map[string]*dbquery.QueryResult{
			"tips": {Columns: []string{"t", "value"}, Rows: rows([]string{"1700000002", "10"})},
		},
	}
	buf, err := json.Marshal(Analyze(cfg, in))
	if err != nil {
		t.Fatal(err)
	}
	out := string(buf)
	for _, want := range []string{
		`"BucketSec":5`, `"S499":1`, `"P50Sec":0.033`, `"OffsetSec":[0]`, `"T50Sec":5`,
		`"RateFromPrev":1`, `"FromPrevSec":{"P50":0.033`, `"Role":"score"`, `"Query":"tips"`,
		`"Query":"lifecycle:viewer"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0.03299999") {
		t.Errorf("JSON has unrounded floats:\n%s", out)
	}
}

func TestParseConfigRejectsUnknownRole(t *testing.T) {
	if _, err := ParseConfig([]byte("series:\n  - {name: x, query: q, role: bonus}\n")); err == nil {
		t.Fatal("want error")
	}
}
