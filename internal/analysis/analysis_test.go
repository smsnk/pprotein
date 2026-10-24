package analysis

import (
	"bytes"
	"strings"
	"testing"

	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/score"
)

const origin = 1_700_000_000.0

func ltsv(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func strp(s string) *string { return &s }

func rows(cells ...[]string) [][]*string {
	out := [][]*string{}
	for _, r := range cells {
		row := []*string{}
		for _, c := range r {
			if c == "NULL" {
				row = append(row, nil)
			} else {
				row = append(row, strp(c))
			}
		}
		out = append(out, row)
	}
	return out
}

func mustConfig(t *testing.T, y string) *Config {
	t.Helper()
	cfg, err := ParseConfig([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNormalizerUsesAlpMatchingGroups(t *testing.T) {
	n, err := NewNormalizer([]byte("matching_groups:\n  - ^/api/livestream/[0-9]+/livecomment$\n  - ^/api/livestream/[0-9]+$\n"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"/api/livestream/12/livecomment?limit=10": "^/api/livestream/[0-9]+/livecomment$",
		"/api/livestream/12":                      "^/api/livestream/[0-9]+$",
		"/api/tag?x=1":                            "/api/tag",
	}
	for in, want := range cases {
		if got := n.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLTSVFallsBackToTimeLocal(t *testing.T) {
	n, _ := NewNormalizer(nil)
	data := ltsv(
		"msec:1700000001.250\tmethod:GET\turi:/a\tstatus:200\treqtime:0.010\tsession:abc",
		"time:14/Nov/2023:22:13:22 +0000\tmethod:GET\turi:/b\tstatus:200\treqtime:0.020\tsession:-",
		"method:GET\turi:/c",
	)
	reqs, skipped := ParseLTSV(data, n, "msec", "session")
	if len(reqs) != 2 || skipped != 1 {
		t.Fatalf("got %d requests, %d skipped", len(reqs), skipped)
	}
	if reqs[0].T != 1700000001.25 || reqs[0].Entity != "abc" {
		t.Errorf("first = %+v", reqs[0])
	}
	if reqs[1].T != 1700000002 || reqs[1].Entity != "" {
		t.Errorf("second = %+v", reqs[1])
	}
}

func TestTimelineBucketsRequestsAndSeries(t *testing.T) {
	cfg := mustConfig(t, `
bucket: 10
series:
  - name: tip
    query: tips
    cumulative: true
`)
	in := &Input{
		Origin: origin,
		Requests: []Request{
			{T: origin + 1, Method: "GET", Endpoint: "/a", Status: 200, ReqTime: 0.1},
			{T: origin + 2, Method: "GET", Endpoint: "/a", Status: 499, ReqTime: 20},
			{T: origin + 25, Method: "POST", Endpoint: "/b", Status: 500, ReqTime: 0.3},
			{T: origin - 5, Method: "GET", Endpoint: "/a", Status: 200},
		},
		Queries: map[string]*dbquery.QueryResult{
			// t は秒・ミリ秒が混ざっていても揃える
			"tips": {Columns: []string{"t", "value"}, Rows: rows(
				[]string{"1700000003", "100"},
				[]string{"1700000004000", "50"},
				[]string{"1700000021", "10"},
			)},
		},
	}
	rep := Analyze(cfg, in)
	b := rep.Timeline.Buckets
	if len(b) != 3 {
		t.Fatalf("got %d buckets, want 3 (0s, 10s, 20s)", len(b))
	}
	if b[0].Requests != 2 || b[0].S2xx != 1 || b[0].Canceled != 1 || b[0].S4xx != 0 {
		t.Errorf("bucket 0 = %+v", b[0])
	}
	if b[1].Requests != 0 {
		t.Errorf("empty bucket must be kept: %+v", b[1])
	}
	if b[2].S5xx != 1 || b[2].Endpoints["POST /b"] != 1 {
		t.Errorf("bucket 2 = %+v", b[2])
	}
	if b[0].Series["tip"] != 150 || b[2].Series["tip"] != 10 {
		t.Errorf("tip series = %v, %v", b[0].Series, b[2].Series)
	}
	if b[1].Series["tip(累積)"] != 150 || b[2].Series["tip(累積)"] != 160 {
		t.Errorf("cumulative = %v, %v", b[1].Series, b[2].Series)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "1 件除外") {
		t.Errorf("warnings = %v", rep.Warnings)
	}
}

// ISUCON14 型: 得点が複数の項の和で、series 列で項ごとに分かれる。
func TestSeriesSplitBySeriesColumn(t *testing.T) {
	cfg := mustConfig(t, "series:\n  - name: score\n    query: score_terms\n")
	in := &Input{Origin: origin, Queries: map[string]*dbquery.QueryResult{
		"score_terms": {Columns: []string{"t", "series", "value"}, Rows: rows(
			[]string{"1700000001.5", "distance", "120"},
			[]string{"1700000002", "completed", "5"},
			[]string{"1700000003", "distance", "30"},
		)},
	}}
	rep := Analyze(cfg, in)
	s := rep.Timeline.Buckets[0].Series
	if s["score:distance"] != 150 || s["score:completed"] != 5 {
		t.Errorf("series = %v", s)
	}
	if got := strings.Join(rep.Timeline.Series, ","); got != "score:distance,score:completed" {
		t.Errorf("series order = %s", got)
	}
}

// ISUCON13 型: セッション cookie で利用者を束ね、エンドポイントの順で進む。
func TestLifecycleFromHTTPLog(t *testing.T) {
	cfg := mustConfig(t, `
httplog:
  entity_key: session
lifecycles:
  - name: viewer
    source: httplog
    steps:
      - name: top
        match: ^GET /api/tag$
      - name: watch
        match: ^GET /api/livestream/
      - name: comment
        match: ^POST /api/livestream/
`)
	req := func(e string, dt float64, m, ep string) Request {
		return Request{T: origin + dt, Method: m, Endpoint: ep, Entity: e, Status: 200}
	}
	in := &Input{Origin: origin, Requests: []Request{
		// a: 最後まで進み、コメントを2回
		req("a", 1, "GET", "/api/tag"),
		req("a", 2, "GET", "/api/livestream/1"),
		req("a", 4, "POST", "/api/livestream/1"),
		req("a", 5, "POST", "/api/livestream/1"),
		// b: 視聴で止まる。順序の前にあるコメントは到達に数えない
		req("b", 1, "POST", "/api/livestream/1"),
		req("b", 2, "GET", "/api/tag"),
		req("b", 6, "GET", "/api/livestream/2"),
		// c: 最初の状態に来ていないので数えない
		req("c", 1, "GET", "/api/livestream/3"),
		// 識別子なしは無視
		req("", 1, "GET", "/api/tag"),
	}}
	rep := Analyze(cfg, in)
	l := rep.Lifecycles[0]
	if l.Entities != 2 {
		t.Fatalf("entities = %d", l.Entities)
	}
	top, watch, comment := l.Steps[0], l.Steps[1], l.Steps[2]
	if top.Reached != 2 || watch.Reached != 2 || comment.Reached != 1 {
		t.Errorf("reached = %d %d %d", top.Reached, watch.Reached, comment.Reached)
	}
	if watch.StoppedHere != 1 || comment.StoppedHere != 1 {
		t.Errorf("stopped = %d %d", watch.StoppedHere, comment.StoppedHere)
	}
	if comment.Rate != 0.5 || comment.PerEntity != 2 {
		t.Errorf("comment = %+v", comment)
	}
	// watch の所要: a は 1s、b は 4s
	if watch.FromPrev == nil || watch.FromPrev.P50 != 1 || watch.FromPrev.Max != 4 {
		t.Errorf("watch.FromPrev = %+v", watch.FromPrev)
	}
	b0 := rep.Timeline.Buckets[0]
	if b0.Series["viewer.started"] != 2 || b0.Series["viewer.finished"] != 1 {
		t.Errorf("lifecycle series = %v", b0.Series)
	}
}

// ISUCON14 型: ライドの状態遷移を SQL から取る。
func TestLifecycleFromQuery(t *testing.T) {
	cfg := mustConfig(t, `
lifecycles:
  - name: ride
    source: query:ride_statuses
    steps:
      - {name: MATCHING, match: ^MATCHING$}
      - {name: ENROUTE, match: ^ENROUTE$}
      - {name: COMPLETED, match: ^COMPLETED$}
`)
	in := &Input{Origin: origin, Queries: map[string]*dbquery.QueryResult{
		"ride_statuses": {Columns: []string{"entity", "state", "t"}, Rows: rows(
			[]string{"r1", "MATCHING", "1700000001.000"},
			[]string{"r1", "ENROUTE", "1700000004.000"},
			[]string{"r1", "COMPLETED", "1700000010.000"},
			[]string{"r2", "MATCHING", "1700000002.000"},
		)},
	}}
	l := Analyze(cfg, in).Lifecycles[0]
	if l.Error != "" || l.Entities != 2 {
		t.Fatalf("lifecycle = %+v", l)
	}
	if l.Steps[0].StoppedHere != 1 || l.Steps[2].Reached != 1 || l.Steps[1].FromPrev.P50 != 3 {
		t.Errorf("steps = %+v", l.Steps)
	}
}

func TestBreakdownShareAndMissingQuery(t *testing.T) {
	cfg := mustConfig(t, `
top: 2
breakdowns:
  - {name: by streamer, query: tips_by_streamer}
  - {name: missing, query: nope}
`)
	in := &Input{Origin: origin, Queries: map[string]*dbquery.QueryResult{
		"tips_by_streamer": {Columns: []string{"key", "value"}, Rows: rows(
			[]string{"alice", "600"}, []string{"bob", "300"}, []string{"carol", "100"},
		)},
	}}
	rep := Analyze(cfg, in)
	b := rep.Breakdowns[0]
	if len(b.Rows) != 2 || b.TotalRows != 3 || b.Share[0] != 0.6 {
		t.Errorf("breakdown = %+v", b)
	}
	if rep.Breakdowns[1].Error == "" {
		t.Error("missing query must be reported")
	}

	var buf bytes.Buffer
	RenderMarkdown(&buf, rep)
	if !strings.Contains(buf.String(), "| alice | 600 | 60.0% |") {
		t.Errorf("markdown:\n%s", buf.String())
	}
}

func TestParseConfigRejectsHTTPLogLifecycleWithoutEntityKey(t *testing.T) {
	_, err := ParseConfig([]byte("lifecycles:\n  - name: x\n    source: httplog\n    steps: [{match: a}]\n"))
	if err == nil {
		t.Fatal("want error")
	}
}

// 見出しのスコアには、ベンチが送ったエラー件数・対象・時間も出す
func TestRenderMarkdownShowsScoreDetails(t *testing.T) {
	var buf bytes.Buffer
	RenderMarkdown(&buf, &Report{GroupId: "A", Score: &score.Brief{Value: 18902, Passed: true, ErrorCount: 3, Target: "isu1", DurationSec: 60}})
	if !strings.Contains(buf.String(), "- スコア: 18902（エラー 3 / 対象 isu1 / 60s）") {
		t.Errorf("markdown:\n%s", buf.String())
	}
}
