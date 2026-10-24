package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func entry(group, status string) Entry {
	e := Entry{Status: status}
	e.Snapshot.GroupId = group
	return e
}

// 収集中の走行は latest に含め、全部が失敗した走行は飛ばす
func TestLatest(t *testing.T) {
	id, err := Latest([]Entry{
		entry("2026-10-11_16-41-34.230854", "ok"),
		entry("2026-10-11_17-02-15.633789", "pending"),
		entry("2026-10-11_17-02-15.633789", "fail"),
		entry("2026-10-11_17-30-00.000000", "fail"),
	})
	if err != nil || id != "2026-10-11_17-02-15.633789" {
		t.Errorf("latest = %q, %v", id, err)
	}
	if _, err := Latest(nil); err == nil {
		t.Error("収集が無ければエラーにする")
	}
}

// サーバーのエラーは echo の JSON から message だけを出す
func TestErrorShowsServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"analysis.yml の httplog.entity_key が空"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL).GetQuery("/api/analysis/x/sessions", nil)
	if err == nil || !strings.HasSuffix(err.Error(), "が 400 を返しました: analysis.yml の httplog.entity_key が空") {
		t.Errorf("err = %v", err)
	}
}

// サーバーに繋がらなければ、残りの収集タイプを読みに行かずに返す（タイムアウトをタイプの数だけ待たない）
func TestAllEntriesStopsWhenUnreachable(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer srv.Close()
	if _, err := New(srv.URL).AllEntries(); err == nil || requests.Load() != 1 {
		t.Errorf("err = %v, requests = %d", err, requests.Load())
	}
}
