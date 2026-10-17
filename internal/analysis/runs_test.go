package analysis

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/summary"
)

func idxEntry(typ, id, gid, status string) summary.Entry {
	e := summary.Entry{Status: status}
	e.Snapshot.Type, e.Snapshot.ID, e.Snapshot.GroupId = typ, id, gid
	return e
}

func TestFingerprintChangesWithMaterialAndSkipsPending(t *testing.T) {
	idx := entryIndex{"G": {idxEntry("httplog", "h1", "G", "ok"), idxEntry("score", "s1", "G", "ok")}}
	base := idx.fingerprint("G", "cfg1")
	if base == "" {
		t.Fatal("complete group must be cacheable")
	}
	if idx.fingerprint("G", "cfg2") == base {
		t.Error("config change must change the key")
	}
	idx["G"] = append(idx["G"], idxEntry("dbquery", "d1", "G", "ok"))
	if idx.fingerprint("G", "cfg1") == base {
		t.Error("a rerun of dbquery must change the key")
	}
	idx["G"] = append(idx["G"], idxEntry("httplog", "h2", "G", "pending"))
	if idx.fingerprint("G", "cfg1") != "" {
		t.Error("pending group must not be cached")
	}
}

func TestSummarizeAndRenderRuns(t *testing.T) {
	rep := &Report{
		GroupId: "2026-10-11_17-02-15.633789",
		Score:   &ScoreInfo{Value: 3385, Passed: true},
		Commit:  &Commit{Hash: "6f32b72aaaaaaa", Message: "msg"},
		Totals: Totals{Requests: 100, S5xx: 1, Canceled: 2, P99: 3.109,
			Endpoints: map[string]int{"GET /icon": 60, "POST /c": 30, "GET /a": 10}},
		SeriesStats: []SeriesStat{
			{Name: "ng_word", Role: "penalty", Agg: "count", Total: 17},
			{Name: "tip", Role: "score", Agg: "sum", Total: 3385},
			{Name: "reaction", Agg: "count", Total: 192},
		},
		Lifecycles: []LifecycleResult{{Name: "viewer", Entities: 20, Steps: []StepResult{
			{Name: "enter", Reached: 20}, {Name: "post", Reached: 17},
		}}},
	}
	s := Summarize(rep)
	if len(s.TopEndpoints) != 3 || s.TopEndpoints[0].Key != "GET /icon" || s.TopEndpoints[0].Share != 0.6 {
		t.Errorf("top endpoints = %+v", s.TopEndpoints)
	}
	if s.Requests.S499 != 2 || s.Lifecycles[0].Steps[1].Reached != 17 {
		t.Errorf("summary = %+v", s)
	}

	var buf bytes.Buffer
	RenderRunsMarkdown(&buf, &RunsResponse{Runs: []RunSummary{s}})
	out := buf.String()
	// 役割の付いた系列だけを score → penalty の順に出す
	if !strings.Contains(out, "| GroupId | commit | score | tip [score] | ng_word [penalty] | req | 5xx | 499 | p99 | viewer 開始→最後 |") {
		t.Errorf("header:\n%s", out)
	}
	if !strings.Contains(out, "| 2026-10-11_17-02-15.633789 | 6f32b72 | 3385 | 3385 | 17 | 100 | 1 | 2 | 3.109 | 20→17 |") {
		t.Errorf("row:\n%s", out)
	}
}

// 時系列は列ごとの配列で出し、読み戻すと同じ Markdown になる（CLI は API の JSON から整形する）。
func TestTimelineJSONIsColumnarAndRoundTrips(t *testing.T) {
	cfg := mustConfig(t, "bucket: 10\nseries:\n  - {name: tip, query: tips, cumulative: true}\n")
	in := &Input{
		Origin: origin,
		Requests: []Request{
			{T: origin + 1, Method: "GET", Endpoint: "/a", Status: 200, ReqTime: 0.1},
			{T: origin + 25, Method: "GET", Endpoint: "/a", Status: 499, ReqTime: 20},
		},
		Queries: map[string]*dbquery.QueryResult{
			"tips": {Columns: []string{"t", "value"}, Rows: rows([]string{"1700000012", "10"})},
		},
	}
	rep := Analyze(cfg, in)
	buf, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	out := string(buf)
	for _, want := range []string{
		`"OffsetSec":[0,10,20]`, `"S499":[0,0,1]`,
		`{"Name":"tip","Values":[null,10,null]}`, `{"Name":"tip(累積)","Values":[0,10,10]}`,
		`{"Name":"GET /a","Values":[1,0,1]}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON lacks %s:\n%s", want, out)
		}
	}

	back := &Report{}
	if err := json.Unmarshal(buf, back); err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	RenderMarkdown(&a, rep)
	RenderMarkdown(&b, back)
	if a.String() != b.String() {
		t.Errorf("round trip changed markdown:\n%s\n---\n%s", a.String(), b.String())
	}
}

func TestMemoEvictsOldest(t *testing.T) {
	m := newMemo[int]()
	for i := 0; i < memoLimit+5; i++ {
		m.put(strconv.Itoa(i), i)
	}
	if _, ok := m.get("0"); ok {
		t.Error("the oldest entry must be evicted")
	}
	if v, ok := m.get(strconv.Itoa(memoLimit + 4)); !ok || v != memoLimit+4 {
		t.Error("the newest entry must be kept")
	}
	if len(m.data) != memoLimit || len(m.order) != memoLimit {
		t.Errorf("size = %d / %d", len(m.data), len(m.order))
	}
	// 同じキーを入れ直しても数は増えない
	m.put(strconv.Itoa(memoLimit+4), 0)
	if len(m.order) != memoLimit {
		t.Errorf("re-put grew the order: %d", len(m.order))
	}
}
