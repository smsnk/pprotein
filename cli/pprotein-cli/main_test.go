package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/client"
)

func TestMain(m *testing.M) {
	pollInterval = time.Millisecond
	os.Exit(m.Run())
}

const (
	older = "2025-11-23_10-21-33.123456"
	newer = "2025-11-23_10-41-02.984213"
	next  = "2025-11-23_11-00-00.000000"

	alpTSV = "Count\tMethod\tUri\tSum\tAvg\tP99\n2\tGET\t/api/a?x=1&y=2\t0.400\t0.200\t0.300\n"
)

// fakeAPI は CLI が読む pprotein の API を真似る。
type fakeAPI struct {
	mu sync.Mutex
	// entries は全タイプのエントリ、bodies はパスごとの本文
	entries []client.Entry
	bodies  map[string]string
	// collect は /api/group/collect を叩かれたときに呼ばれる
	collect func(f *fakeAPI)
	// settle は pending のエントリが ok になるまでのポーリングの回数（0 なら変えない）
	settle int
	// down なら一覧を 500 にする
	down bool
}

// newFake は older（確定。summary に節の無い ptqd だけ失敗）と newer（全部失敗）の2つの収集を持つ。
func newFake() *fakeAPI {
	return &fakeAPI{
		entries: []client.Entry{entry("httplog", older, "ok"), entry("ptqd", older, "fail"), entry("httplog", newer, "fail")},
		bodies:  map[string]string{"/api/httplog/" + older + "-httplog": alpTSV},
	}
}

func entry(typ, group, status string) client.Entry {
	e := client.Entry{Status: status}
	e.Snapshot.Type, e.Snapshot.ID, e.Snapshot.GroupId, e.Snapshot.Label = typ, group+"-"+typ, group, "isu1"
	return e
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.URL.Path == "/api/group/collect" {
		f.collect(f)
		return
	}
	if typ := strings.TrimPrefix(r.URL.Path, "/api/"); !strings.Contains(typ, "/") {
		if f.down {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		// AllEntries は1回のポーリングで Types[0] から順に読む
		if typ == client.Types[0] && f.settle > 0 {
			if f.settle--; f.settle == 0 {
				for i := range f.entries {
					f.entries[i].Status = "ok"
				}
			}
		}
		list := []client.Entry{}
		for _, e := range f.entries {
			if e.Snapshot.Type == typ {
				list = append(list, e)
			}
		}
		json.NewEncoder(w).Encode(list)
		return
	}
	body, ok := f.bodies[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	io.WriteString(w, body)
}

func run(t *testing.T, f *fakeAPI, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append(args, "--url", srv.URL))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

// latest は全部が失敗した収集を飛ばし、飛ばしたことを stderr に書く。古い名前でも動き、新しい名前を案内する
func TestLatestAliasSkipsFailedGroup(t *testing.T) {
	out, stderr, err := run(t, newFake(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	// summary に節の無い ptqd の失敗は警告しない
	if !strings.HasPrefix(out, "# summary "+older) || strings.Contains(out, "ptqd") {
		t.Errorf("stdout:\n%s", out)
	}
	if !strings.Contains(stderr, "`pprotein-cli summary` を使ってください") ||
		!strings.Contains(stderr, "latest: "+newer+" は全部の収集に失敗したので、"+older+" を使います") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

// AFTER を省くと latest と比べる。diff は pprof を読まない
func TestDiffDefaultsAfterToLatest(t *testing.T) {
	f := newFake()
	f.entries = append(f.entries, entry("pprof", older, "ok")) // 中身は無い（読みに行けば警告になる）
	out, _, err := run(t, f, "summary", "diff", older)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# summary diff "+older+" -> "+older) || strings.Contains(out, "中身を読めなかった") {
		t.Errorf("stdout:\n%s", out)
	}
}

// --wait は、そのコマンドが読むエントリだけを待つ（pt-query-digest の処理は数分かかることがある）
func TestWaitOnlyForWhatTheCommandReads(t *testing.T) {
	f := newFake()
	f.entries = append(f.entries, entry("httplog", next, "ok"), entry("ptqd", next, "pending"))
	f.bodies["/api/httplog/"+next+"-httplog"] = alpTSV
	f.bodies["/api/analysis/"+next] = `{"GroupId":"` + next + `"}`
	for _, cmd := range []string{"summary", "analyze"} {
		out, _, err := run(t, f, cmd, next, "--wait", "--timeout", "1s")
		if err != nil || !strings.HasPrefix(out, "# "+cmd+" "+next) {
			t.Errorf("%s --wait: err = %v, stdout:\n%s", cmd, err, out)
		}
	}
}

func TestOldNamesAreHiddenAliases(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"latest", "diff", "analyze-diff", "analyze-trend", "endpoints", "sessions", "session", "transitions"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || !cmd.Hidden {
			t.Errorf("%s: cmd = %v, err = %v", name, cmd.Name(), err)
		}
	}
}

// endpoints も収集中・失敗の警告を出し、JSON は収集の素性を持つ
func TestEndpointsKeepWarnings(t *testing.T) {
	f := newFake()
	f.bodies["/api/analysis/"+older] = `{"GroupId":"` + older + `","Warnings":["httplog isu2 は収集中（pending）"],"EndpointStats":[{"Key":"GET /api/a","Count":2}]}`
	out, _, err := run(t, f, "analyze", "endpoints")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- **警告**: httplog isu2 は収集中") || !strings.Contains(out, "| GET /api/a | 2 |") {
		t.Errorf("stdout:\n%s", out)
	}
	out, _, err = run(t, f, "analyze", "endpoints", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, `{"GroupId":"`+older+`","Warnings":["httplog isu2`) {
		t.Errorf("stdout:\n%s", out)
	}
}

// analyze も CLI 側で latest を解決する（サーバーの latest とは意味が違う）
func TestAnalyzeResolvesLatestOnCLI(t *testing.T) {
	f := newFake()
	f.bodies["/api/analysis/"+older] = `{"GroupId":"` + older + `","Origin":"2025-11-23T10:21:33Z","BucketSec":5}`
	out, _, err := run(t, f, "analyze")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# analyze "+older) {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestFlagsBelongToTheirCommand(t *testing.T) {
	_, _, err := run(t, newFake(), "summary", "--lifecycle", "viewer")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --lifecycle") {
		t.Errorf("err = %v", err)
	}
}

func TestMistypedSubcommandIsNotAGroup(t *testing.T) {
	_, _, err := run(t, newFake(), "analyze", "sesions")
	if err == nil || !strings.Contains(err.Error(), "サブコマンドなら sessions") {
		t.Errorf("err = %v", err)
	}
}

func TestJSONIsOneLine(t *testing.T) {
	out, _, err := run(t, newFake(), "summary", older, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, `"SumSec":0.4`) || !strings.Contains(out, `"Uri":"/api/a?x=1&y=2"`) {
		t.Errorf("stdout:\n%s", out)
	}
}

// httplog が先に確定しても、ptqd などほかのタイプも確定するまで待つ
func TestCollectWaitsForNewGroup(t *testing.T) {
	f := newFake()
	f.collect = func(f *fakeAPI) {
		f.entries = append(f.entries, entry("httplog", next, "ok"), entry("ptqd", next, "pending"))
		f.bodies["/api/httplog/"+next+"-httplog"] = alpTSV
		f.settle = 2
	}
	out, stderr, err := run(t, f, "collect")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# summary "+next) {
		t.Errorf("stdout:\n%s", out)
	}
	if !strings.Contains(stderr, "収集中 ("+next+"): 残り 1 件") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

// サーバーが落ちたら、--timeout まで待たずに止める
func TestCollectGivesUpWhenServerIsDown(t *testing.T) {
	f := newFake()
	f.collect = func(f *fakeAPI) { f.down = true }
	_, _, err := run(t, f, "collect")
	if err == nil || !strings.Contains(err.Error(), "収集の状態を読めません") {
		t.Errorf("err = %v", err)
	}
}
