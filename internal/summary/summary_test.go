package summary

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/pprof/profile"
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
	// flat は葉にだけ付く
	if funcs[0].Name != "main.handleNotification" || funcs[0].Flat != 1.5e9 {
		t.Errorf("葉 = %+v", funcs[0])
	}
	if funcs[0].FlatPct != 100 {
		t.Errorf("FlatPct = %v, want 100", funcs[0].FlatPct)
	}
	// 呼び出し元は flat 0 / cum あり
	if funcs[1].Flat != 0 || funcs[1].Cum != 1.5e9 {
		t.Errorf("呼び出し元 = %+v", funcs[1])
	}
	if funcs[0].Unit != "nanoseconds" {
		t.Errorf("Unit = %q", funcs[0].Unit)
	}
}

// mockServer は pprotein の API を最小限だけ真似る。
func mockServer(t *testing.T, groups map[string]map[string]string, scores map[string]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	for _, typ := range CollectTypes {
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

	g, err := Load(NewClient(s.URL), "", 2)
	if err != nil {
		t.Fatal(err)
	}

	if g.ID != "2025-11-23_10-41-02.984213" {
		t.Errorf("ID = %q（最新が選ばれていない）", g.ID)
	}
	if g.Score == nil || g.Score.Score != 18902 {
		t.Errorf("Score = %+v", g.Score)
	}
	if g.Repository == nil || g.Repository.Hash != "a1b2c3d4e5f6" {
		t.Errorf("Repository = %+v", g.Repository)
	}
	if len(g.HTTPLog) != 1 || len(g.HTTPLog[0].Endpoints) != 2 {
		t.Errorf("--top が効いていない: %+v", g.HTTPLog)
	}

	md := g.Markdown()
	for _, want := range []string{
		"# group 2025-11-23_10-41-02.984213",
		"score=18902 pass errors=3",
		`a1b2c3d "notification のキャッシュ"`,
		"## httplog: isu1",
		"/api/chair/rides/abc123",
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
	c := NewClient(s.URL)

	before, err := Load(c, "2025-11-23_10-21-33.123456", 0)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Load(c, "2025-11-23_10-41-02.984213", 0)
	if err != nil {
		t.Fatal(err)
	}

	d := Compare(before, after, 10)
	if d.Score == nil || d.Score.Before != 12345 || d.Score.After != 18902 {
		t.Fatalf("Score = %+v", d.Score)
	}

	byKey := map[string]EndpointDiff{}
	for _, e := range d.Endpoints {
		byKey[e.Key] = e
	}
	// 改善したもの
	if e := byKey["GET /api/app/notification"]; e.SumBefore != 0.46 || e.SumAfter != 0.1 {
		t.Errorf("notification = %+v", e)
	}
	// 悪化したもの
	if e := byKey["POST /api/app/rides"]; e.SumDelta() != 0.25 {
		t.Errorf("rides delta = %v, want 0.25", e.SumDelta())
	}
	// after から消えたものも残す
	if e, ok := byKey["GET /api/chair/rides/abc123"]; !ok || e.SumAfter != 0 {
		t.Errorf("消えたエンドポイントが残っていない: %+v", byKey)
	}
	// 増減の大きい順
	if d.Endpoints[0].Key != "GET /api/chair/rides/abc123" {
		t.Errorf("並び順 = %+v", d.Endpoints)
	}

	md := d.Markdown()
	for _, want := range []string{
		"# diff 2025-11-23_10-21-33.123456 -> 2025-11-23_10-41-02.984213",
		"score: 12345 -> 18902 (+6557, +53.1%)",
		"## httplog",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown に %q が無い:\n%s", want, md)
		}
	}
}

func TestLoadMissingGroup(t *testing.T) {
	s := mockServer(t, map[string]map[string]string{}, map[string]string{})
	if _, err := Load(NewClient(s.URL), "", 10); err == nil {
		t.Error("収集が無いときはエラーにする")
	}
	if _, err := Load(NewClient(s.URL), "nosuch", 10); err == nil {
		t.Error("存在しない group はエラーにする")
	}
}
