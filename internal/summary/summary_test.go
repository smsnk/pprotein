package summary

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/google/pprof/profile"
	"github.com/smsnk/pprotein/internal/client"
)

// alp v1.0.21 に --format tsv を実行させた実際の出力。
const alpTSV = "Count\t1xx\t2xx\t3xx\t4xx\t5xx\tMethod\tUri\tMin\tMax\tSum\tAvg\tP90\tP95\tP99\tStddev\tMin(Body)\tMax(Body)\tSum(Body)\tAvg(Body)\n" +
	"2\t0\t2\t0\t0\t0\tGET\t/api/app/notification\t0.120\t0.340\t0.460\t0.230\t0.340\t0.340\t0.340\t0.110\t512.000\t512.000\t1024.000\t512.000\n" +
	"1\t0\t1\t0\t0\t0\tGET\t/api/chair/rides/abc123\t0.900\t0.900\t0.900\t0.900\t0.900\t0.900\t0.900\t0.000\t128.000\t128.000\t128.000\t128.000\n" +
	"1\t0\t1\t0\t0\t0\tPOST\t/api/app/rides\t0.050\t0.050\t0.050\t0.050\t0.050\t0.050\t0.050\t0.000\t64.000\t64.000\t64.000\t64.000\n"

// slp v0.2.1 の実際の出力。
const slpTSV = "Count\tQuery\tMin(QueryTime)\tMax(QueryTime)\tSum(QueryTime)\tAvg(QueryTime)\tMin(LockTime)\tMax(LockTime)\tSum(LockTime)\tAvg(LockTime)\tMin(RowsSent)\tMax(RowsSent)\tSum(RowsSent)\tAvg(RowsSent)\tMin(RowsExamined)\tMax(RowsExamined)\tSum(RowsExamined)\tAvg(RowsExamined)\n" +
	"1\tSELECT COUNT(N) FROM `chairs` WHERE `is_active` = N\t0.900000\t0.900000\t0.900000\t0.900000\t0.000100\t0.000100\t0.000100\t0.000100\t1\t1\t1\t1.000000\t900000\t900000\t900000\t900000.000000\n" +
	"2\tSELECT * FROM `rides` WHERE `user_id` = N ORDER BY `created_at` DESC\t0.150000\t0.350000\t0.500000\t0.250000\t0.000100\t0.000100\t0.000200\t0.000100\t1\t1\t2\t1.000000\t100\t120000\t120100\t60050.000000\n"

func TestParseEndpoints(t *testing.T) {
	got := ParseEndpoints(alpTSV)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	// SUM の大きい順
	if got[0].Uri != "/api/chair/rides/abc123" {
		t.Errorf("先頭 = %+v, SUM の大きい順ではない", got[0])
	}
	e := got[1]
	if e.Uri != "/api/app/notification" || e.Method != "GET" {
		t.Errorf("%+v", e)
	}
	if e.Count != 2 || e.Sum != 0.46 || e.Avg != 0.23 || e.P99 != 0.34 {
		t.Errorf("count=%v sum=%v avg=%v p99=%v", e.Count, e.Sum, e.Avg, e.P99)
	}
}

func TestParseQueries(t *testing.T) {
	got := ParseQueries(slpTSV)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	// Sum(QueryTime) の大きい順
	if !strings.HasPrefix(got[0].Query, "SELECT COUNT") {
		t.Errorf("先頭 = %+v", got[0])
	}
	if got[0].Sum != 0.9 || got[0].Count != 1 || got[0].RowsExamined != 900000 {
		t.Errorf("%+v", got[0])
	}
	if got[1].Sum != 0.5 || got[1].Avg != 0.25 || got[1].RowsExamined != 60050 {
		t.Errorf("%+v", got[1])
	}
}

func TestParseTSVIgnoresBrokenRows(t *testing.T) {
	if got := ParseEndpoints("Count\tUri\n"); len(got) != 0 {
		t.Errorf("ヘッダだけなら空: %+v", got)
	}
	if got := ParseEndpoints(""); len(got) != 0 {
		t.Errorf("空文字なら空: %+v", got)
	}
	// Uri 列が無い行は落とす
	if got := ParseEndpoints("Count\tSum\n1\t2\n"); len(got) != 0 {
		t.Errorf("Uri の無い行は落とす: %+v", got)
	}
}

// buildProfile は葉が leaf、その呼び出し元が caller のサンプルを1つ持つ CPU プロファイルを作る。
func buildProfile(t *testing.T, leaf, caller string, ns int64) []byte {
	t.Helper()

	fnLeaf := &profile.Function{ID: 1, Name: leaf}
	fnCaller := &profile.Function{ID: 2, Name: caller}
	locLeaf := &profile.Location{ID: 1, Line: []profile.Line{{Function: fnLeaf}}}
	locCaller := &profile.Location{ID: 2, Line: []profile.Line{{Function: fnCaller}}}

	p := &profile.Profile{
		SampleType: []*profile.ValueType{
			{Type: "samples", Unit: "count"},
			{Type: "cpu", Unit: "nanoseconds"},
		},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     1,
		Function:   []*profile.Function{fnLeaf, fnCaller},
		Location:   []*profile.Location{locLeaf, locCaller},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{locLeaf, locCaller}, Value: []int64{1, ns}},
		},
	}

	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseProfile(t *testing.T) {
	raw := buildProfile(t, "main.handleNotification", "main.(*Handler).ServeHTTP", 1_500_000_000)

	funcs, err := ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 2 {
		t.Fatalf("funcs = %+v", funcs)
	}
	// flat は葉にだけ付く。時間は秒に直す
	if funcs[0].Name != "main.handleNotification" || funcs[0].Flat != 1.5 {
		t.Errorf("葉 = %+v", funcs[0])
	}
	if funcs[0].FlatShare != 1 {
		t.Errorf("FlatShare = %v, want 1", funcs[0].FlatShare)
	}
	// 呼び出し元は flat 0 / cum あり
	if funcs[1].Flat != 0 || funcs[1].Cum != 1.5 {
		t.Errorf("呼び出し元 = %+v", funcs[1])
	}
	if funcs[0].Unit != "seconds" {
		t.Errorf("Unit = %q", funcs[0].Unit)
	}
}

// inline された関数は1つの Location に Line として並ぶ。flat は Line[0]（最も内側）にだけ付く。
func TestParseProfileInlined(t *testing.T) {
	leaf := &profile.Function{ID: 1, Name: "leaf"}
	caller := &profile.Function{ID: 2, Name: "caller"}
	other := &profile.Function{ID: 3, Name: "other"}
	inlined := &profile.Location{ID: 1, Line: []profile.Line{{Function: leaf}, {Function: caller}}}
	plain := &profile.Location{ID: 2, Line: []profile.Line{{Function: other}}}
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "samples", Unit: "count"}, {Type: "cpu", Unit: "nanoseconds"}},
		Function:   []*profile.Function{leaf, caller, other},
		Location:   []*profile.Location{inlined, plain},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{inlined}, Value: []int64{1, 100}},
			{Location: []*profile.Location{plain}, Value: []int64{1, 100}},
		},
	}
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatal(err)
	}

	funcs, err := ParseProfile(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Func{}
	for _, f := range funcs {
		byName[f.Name] = f
	}
	if f := byName["leaf"]; f.Flat != 100e-9 || f.FlatShare != 0.5 {
		t.Errorf("leaf = %+v", f)
	}
	if f := byName["caller"]; f.Flat != 0 || f.Cum != 100e-9 {
		t.Errorf("caller（inline された呼び出し元）= %+v", f)
	}
	if f := byName["other"]; f.FlatShare != 0.5 {
		t.Errorf("other = %+v", f)
	}
}

// mockServer は pprotein の API を最小限だけ真似る。
func mockServer(t *testing.T, groups map[string]map[string]string, scores map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	for _, typ := range client.Types {
		typ := typ
		mux.HandleFunc("/api/"+typ, func(w http.ResponseWriter, r *http.Request) {
			entries := []string{}
			for gid, byType := range groups {
				if _, ok := byType[typ]; !ok {
					continue
				}
				entries = append(entries, fmt.Sprintf(
					`{"Snapshot":{"Type":%q,"ID":"%s-%s","Datetime":"2025-11-23T10:41:02Z",`+
						`"Repository":{"Hash":"a1b2c3d4e5f6","Message":"notification のキャッシュ\n","Ref":"refs/heads/12-cache"},`+
						`"GroupId":%q,"Label":"isu1"},"Status":"ok","Message":"Ready"}`,
					typ, gid, typ, gid))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, "[%s]", strings.Join(entries, ","))
		})
	}
	// /api/<type>/<id> は処理済みの中身
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		typ, id := parts[0], parts[len(parts)-1]
		gid := strings.TrimSuffix(id, "-"+typ)
		if typ == "score" {
			body, ok := scores[gid]
			if !ok {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, body)
			return
		}
		body, ok := groups[gid][typ]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestLoadAndMarkdown(t *testing.T) {
	s := mockServer(t,
		map[string]map[string]string{
			"2025-11-23_10-41-02.984213": {
				"httplog":  alpTSV,
				"slowlog":  slpTSV,
				"resource": "host: isu1 (4 cpu)\n[host cpu %]\nbusy 92.4\n",
				"score":    "",
			},
		},
		map[string]string{
			"2025-11-23_10-41-02.984213": `{"Score":18902,"Passed":true,"Target":"isu1","ErrorCount":3}`,
		},
	)

	g, err := Load(client.New(s.URL), "2025-11-23_10-41-02.984213", 2)
	if err != nil {
		t.Fatal(err)
	}

	if g.Score == nil || g.Score.Value != 18902 {
		t.Errorf("Score = %+v", g.Score)
	}
	if g.Commit == nil || g.Commit.Hash != "a1b2c3d4e5f6" || g.Commit.Message != "notification のキャッシュ" {
		t.Errorf("Commit = %+v", g.Commit)
	}
	if len(g.HTTPLog) != 1 || len(g.HTTPLog[0].Endpoints) != 2 {
		t.Errorf("--top が効いていない: %+v", g.HTTPLog)
	}

	md := g.Markdown()
	for _, want := range []string{
		"# summary 2025-11-23_10-41-02.984213",
		"- スコア: 18902\n",
		`- コミット: a1b2c3d "notification のキャッシュ" (refs/heads/12-cache)`,
		"## httplog: isu1（SUM の大きい順に 2 件）",
		"| 1 | GET | /api/chair/rides/abc123 | 0.900s | 0.900s | 0.900s |",
		"## slowlog: isu1",
		"ROWS EXAMINED",
		"## resource: isu1",
		"busy 92.4",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown に %q が無い:\n%s", want, md)
		}
	}
	// 表がクエリ内の改行や | で壊れないこと
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| ") && strings.Count(line, "\n") > 0 {
			t.Errorf("表の行に改行が混ざっている: %q", line)
		}
	}
}

func TestCompare(t *testing.T) {
	// after では notification の SUM が減り、rides が増えている
	afterAlp := "Count\tMethod\tUri\tSum\tAvg\tP99\n" +
		"2\tGET\t/api/app/notification\t0.100\t0.050\t0.060\n" +
		"1\tPOST\t/api/app/rides\t0.300\t0.300\t0.300\n"

	s := mockServer(t,
		map[string]map[string]string{
			"2025-11-23_10-21-33.123456": {"httplog": alpTSV, "slowlog": slpTSV, "score": ""},
			"2025-11-23_10-41-02.984213": {"httplog": afterAlp, "score": ""},
		},
		map[string]string{
			"2025-11-23_10-21-33.123456": `{"Score":12345,"Passed":true}`,
			"2025-11-23_10-41-02.984213": `{"Score":18902,"Passed":true}`,
		},
	)
	c := client.New(s.URL)

	before, err := Load(c, "2025-11-23_10-21-33.123456", 0)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Load(c, "2025-11-23_10-41-02.984213", 0)
	if err != nil {
		t.Fatal(err)
	}

	d := Compare(before, after, 10)
	if d.Score == nil || d.Score.Before.Value != 12345 || d.Score.After.Value != 18902 {
		t.Fatalf("Score = %+v", d.Score)
	}

	byKey := map[string]EndpointDiff{}
	for _, e := range d.Endpoints {
		byKey[e.Key] = e
	}
	// 改善したもの
	if e := byKey["GET /api/app/notification"]; e.Before.Sum != 0.46 || e.After.Sum != 0.1 {
		t.Errorf("notification = %+v", e)
	}
	// 悪化したもの
	if e := byKey["POST /api/app/rides"]; e.After.Sum-e.Before.Sum != 0.25 {
		t.Errorf("rides delta = %v, want 0.25", e.After.Sum-e.Before.Sum)
	}
	// after から消えたものも残し、SUM がまるごと減ったものとして数える
	if e, ok := byKey["GET /api/chair/rides/abc123"]; !ok || e.After != nil || e.Impact != -0.9 {
		t.Errorf("消えたエンドポイント = %+v", e)
	}
	// IMPACT の大きい順
	if d.Endpoints[0].Key != "GET /api/chair/rides/abc123" {
		t.Errorf("並び順 = %+v", d.Endpoints)
	}
	// after に slowlog が丸ごと無くても、before のクエリは削除として残す
	if len(d.Queries) != 2 || d.Queries[0].After != nil || d.Queries[0].Label != "isu1" {
		t.Errorf("Queries = %+v", d.Queries)
	}

	md := d.Markdown()
	for _, want := range []string{
		"# summary diff 2025-11-23_10-21-33.123456 -> 2025-11-23_10-41-02.984213",
		"- スコア: 12345 -> 18902 (+6557, +53.1%)",
		`- コミット: a1b2c3d "notification のキャッシュ" (refs/heads/12-cache) -> a1b2c3d "notification のキャッシュ" (refs/heads/12-cache)`,
		"## httplog（IMPACT の大きい順）",
		"| isu1 | GET /api/chair/rides/abc123 | 1 → （なし） | 0.900s → （なし） | 0.900s → （なし） | -0.900s（削除） |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown に %q が無い:\n%s", want, md)
		}
	}

	// JSON には GroupId だけを載せ、収集の中身（全エンドポイント・プロファイル）は載せない
	buf, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), `"Before":"2025-11-23_10-21-33.123456"`) || strings.Contains(string(buf), "HTTPLog") ||
		!strings.Contains(string(buf), `"ImpactSec":-0.9`) || !strings.Contains(string(buf), `"Commit":{"Before":{"Hash":"a1b2c3d4e5f6"`) ||
		strings.Contains(string(buf), `"Key"`) {
		t.Errorf("JSON = %s", buf)
	}
}

// 固定時間のベンチでは SUM がほとんど動かず、代わりに件数が増える。
// SUM の増減で並べると、いちばん効いたエンドポイントが下に埋もれてしまう。
func TestCompareRanksByImpactNotSumDelta(t *testing.T) {
	// hot: 1件あたり 0.033s -> 0.010s、件数が 14394 -> 44529 に増えた（大きく効いた）
	// cold: 1件あたりは同じくらい改善したが、そもそも件数が少ない
	beforeAlp := "Count\tMethod\tUri\tSum\tAvg\n" +
		"14394\tGET\t/hot\t468.961\t0.033\n" +
		"159\tGET\t/cold\t5.287\t0.033\n"
	afterAlp := "Count\tMethod\tUri\tSum\tAvg\n" +
		"44529\tGET\t/hot\t465.864\t0.010\n" +
		"193\tGET\t/cold\t1.986\t0.010\n"

	s := mockServer(t, map[string]map[string]string{
		"2025-11-23_10-21-33.123456": {"httplog": beforeAlp},
		"2025-11-23_10-41-02.984213": {"httplog": afterAlp},
	}, map[string]string{})
	c := client.New(s.URL)

	before, err := Load(c, "2025-11-23_10-21-33.123456", 0)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Load(c, "2025-11-23_10-41-02.984213", 0)
	if err != nil {
		t.Fatal(err)
	}

	d := Compare(before, after, 10)

	// SUM の増減だけ見ると cold(-3.301) > hot(-3.097) で順位が逆転する
	sumDelta := func(e EndpointDiff) float64 { return math.Abs(e.After.Sum - e.Before.Sum) }
	if !(sumDelta(d.Endpoints[0]) < sumDelta(d.Endpoints[1])) {
		t.Fatalf("この前提が崩れるとテストの意味がない: %+v", d.Endpoints)
	}
	if d.Endpoints[0].Key != "GET /hot" {
		t.Errorf("件数の多いエンドポイントが先頭に来るはず: %+v", d.Endpoints)
	}
	if d.Endpoints[0].Impact >= -100 {
		t.Errorf("hot の IMPACT = %v, もっと大きな削減のはず", d.Endpoints[0].Impact)
	}
}

// alp の AVG は小数3桁に丸められている。1ms 前後のエンドポイントの改善が IMPACT 0 にならないように SUM / COUNT を使う
func TestImpactUsesSumOverCount(t *testing.T) {
	// 1.46ms → 0.73ms。alp の AVG はどちらも 0.001
	before := map[string][]Endpoint{"isu1": {{Method: "GET", Uri: "/fast", Count: 1024, Sum: 1.5, Avg: 0.001}}}
	after := map[string][]Endpoint{"isu1": {{Method: "GET", Uri: "/fast", Count: 1024, Sum: 0.75, Avg: 0.001}}}
	if got := compareRows(before, after, 0)[0].Impact; got != -0.75 {
		t.Errorf("Impact = %v, want -0.75", got)
	}
}

// 同じ値の行は名前・label・キーの順に並べ、実行ごとに並びが変わらない
func TestTiesAreStable(t *testing.T) {
	fns := []*profile.Function{{ID: 1, Name: "c"}, {ID: 2, Name: "a"}, {ID: 3, Name: "b"}}
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		Function:   fns,
	}
	for i, fn := range fns {
		loc := &profile.Location{ID: uint64(i + 1), Line: []profile.Line{{Function: fn}}}
		p.Location = append(p.Location, loc)
		p.Sample = append(p.Sample, &profile.Sample{Location: []*profile.Location{loc}, Value: []int64{10}})
	}
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatal(err)
	}

	same := func(count float64) []Endpoint {
		return []Endpoint{{Method: "GET", Uri: "/b", Count: count, Sum: 1, Avg: 1}, {Method: "GET", Uri: "/a", Count: count, Sum: 1, Avg: 1}}
	}
	before := map[string][]Endpoint{"isu2": same(1), "isu1": same(1)}

	// map を回す順は毎回変わるので、何度か試す
	for i := 0; i < 20; i++ {
		funcs, err := ParseProfile(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if got := funcs[0].Name + funcs[1].Name + funcs[2].Name; got != "abc" {
			t.Fatalf("pprof の並び = %s", got)
		}

		keys := []string{}
		for _, d := range compareRows(before, map[string][]Endpoint{}, 0) {
			keys = append(keys, d.Label+" "+d.Key)
		}
		if got := strings.Join(keys, ", "); got != "isu1 GET /a, isu1 GET /b, isu2 GET /a, isu2 GET /b" {
			t.Fatalf("diff の並び = %s", got)
		}
	}
}

func TestLoadMissingGroup(t *testing.T) {
	s := mockServer(t, map[string]map[string]string{}, map[string]string{})
	if _, err := Load(client.New(s.URL), "nosuch", 10); err == nil {
		t.Error("存在しない group はエラーにする")
	}
}

// 中身を読めなかったエントリは黙って落とさず警告に出す
func TestLoadReportsUnreadableEntries(t *testing.T) {
	s := mockServer(t,
		map[string]map[string]string{
			"2025-11-23_10-41-02.984213": {"httplog": alpTSV, "score": ""},
		},
		map[string]string{}, // score の中身が無い（404）
	)
	g, err := Load(client.New(s.URL), "2025-11-23_10-41-02.984213", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Warnings) != 1 || !strings.HasPrefix(g.Warnings[0], "score isu1 の中身を読めなかった（") {
		t.Errorf("Warnings = %v", g.Warnings)
	}
	if len(g.HTTPLog) != 1 {
		t.Errorf("読めたものは残す: %+v", g.HTTPLog)
	}
}

func TestLoadOnlyGivenTypes(t *testing.T) {
	s := mockServer(t,
		map[string]map[string]string{
			"2025-11-23_10-41-02.984213": {"httplog": alpTSV, "slowlog": slpTSV, "resource": "busy 92.4\n"},
		},
		map[string]string{},
	)
	g, err := Load(client.New(s.URL), "2025-11-23_10-41-02.984213", 10, "httplog")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.HTTPLog) != 1 || g.SlowLog != nil || g.Resource != nil {
		t.Errorf("httplog だけ読むはず: %+v", g)
	}
}

// 同じ収集にスコアが複数あれば、一覧の順によらず新しいものを使う
func TestLoadUsesNewestScore(t *testing.T) {
	const group = "2025-11-23_10-41-02.984213"
	entry := func(id, at string) string {
		return fmt.Sprintf(`{"Snapshot":{"Type":"score","ID":%q,"Datetime":%q,"GroupId":%q,"Label":"bench"},"Status":"ok"}`, id, at, group)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/score", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "[%s,%s]", entry("new", "2025-11-23T10:45:00Z"), entry("old", "2025-11-23T10:43:00Z"))
	})
	mux.HandleFunc("/api/score/old", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"Score":1}`) })
	mux.HandleFunc("/api/score/new", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"Score":2,"Passed":true}`) })
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)

	g, err := Load(client.New(s.URL), group, 10)
	if err != nil {
		t.Fatal(err)
	}
	if g.Score == nil || g.Score.Value != 2 {
		t.Errorf("Score = %+v", g.Score)
	}
}
