package analysis

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/client"
)

const group = "2026-10-11_17-02-15.633789"

func entry(id, groupId, label, status, msg string) client.Entry {
	e := client.Entry{Status: status, Message: msg}
	e.Snapshot.ID = id
	e.Snapshot.GroupId = groupId
	e.Snapshot.Label = label
	return e
}

// fakeAPI は Load が読む pprotein の API だけを返す。bodies はパスごとの本文（生ログ・dbquery の結果）。
func fakeAPI(t *testing.T, httplogs, dbqueries []client.Entry, bodies map[string]string) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Error(err)
		}
	}
	// 本物の API と同じく、一覧のエントリには収集タイプが入っている
	for i := range httplogs {
		httplogs[i].Snapshot.Type = "httplog"
	}
	for i := range dbqueries {
		dbqueries[i].Snapshot.Type = "dbquery"
	}
	mux.HandleFunc("/api/httplog", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, httplogs) })
	mux.HandleFunc("/api/dbquery", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, dbqueries) })
	mux.HandleFunc("/api/httplog/config", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return client.New(srv.URL)
}

// 走行直後は httplog の収集（Duration 110s）が終わっていない。飛ばしたことを警告に出す。
func TestLoadWarnsPendingAndFailedEntries(t *testing.T) {
	cfg := mustConfig(t, "httplog:\n  label: isu1\n")
	api := fakeAPI(t,
		[]client.Entry{
			entry("h1", group, "isu1", "pending", "Collecting"),
			entry("h2", group, "isu2", "pending", "Collecting"), // label が違うので対象外
			entry("h0", "2026-10-11_16-41-34.230854", "isu1", "ok", "Ready"),
		},
		[]client.Entry{
			entry("d1", group, "", "fail", "dial tcp: connection refused"),
		},
		nil,
	)
	in, err := Load(api, cfg, group)
	if err != nil {
		t.Fatal(err)
	}
	rep := Analyze(cfg, in)
	if len(rep.Warnings) != 2 {
		t.Fatalf("warnings = %v", rep.Warnings)
	}
	if !strings.Contains(rep.Warnings[0], "httplog isu1 は収集中（pending）") {
		t.Errorf("httplog warning = %q", rep.Warnings[0])
	}
	if !strings.Contains(rep.Warnings[1], "dbquery は収集に失敗した（dial tcp: connection refused）") {
		t.Errorf("dbquery warning = %q", rep.Warnings[1])
	}
}

// 確定した httplog だけなら警告は出ない。dbquery の失敗は、再実行で成功していれば無視する。
func TestLoadIgnoresDBQueryFailureRetried(t *testing.T) {
	cfg := mustConfig(t, "httplog:\n  entity_key: session\n")
	api := fakeAPI(t,
		[]client.Entry{entry("h1", group, "isu1", "ok", "Ready")},
		[]client.Entry{
			entry("d1", group, "", "fail", "timeout"),
			entry("d2", group, "", "ok", "Ready"),
		},
		map[string]string{
			"/api/httplog/data/h1": "msec:1700000001.000\tmethod:GET\turi:/a\tstatus:200\treqtime:0.010\tsession:abc\n",
			"/api/dbquery/d2":      `{"Queries":[]}`,
		},
	)
	in, err := Load(api, cfg, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Warnings) != 0 || len(in.Requests) != 1 {
		t.Errorf("warnings = %v, requests = %d", in.Warnings, len(in.Requests))
	}
}

func TestLatestGroupIncludesPending(t *testing.T) {
	api := fakeAPI(t,
		[]client.Entry{
			entry("h0", "2026-10-11_16-41-34.230854", "isu1", "ok", "Ready"),
			entry("h1", group, "isu1", "pending", "Collecting"),
			entry("h2", "2026-10-11_17-30-00.000000", "isu1", "fail", "error"),
		},
		nil, nil,
	)
	id, err := LatestGroup(api)
	if err != nil || id != group {
		t.Errorf("latest = %q, %v", id, err)
	}
}
