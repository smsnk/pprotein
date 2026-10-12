package analysis

import (
	"strings"
	"testing"
)

func sessionInput() (*Config, *Input) {
	cfg, _ := ParseConfig([]byte(`
httplog:
  entity_key: session
lifecycles:
  - name: viewer
    source: httplog
    steps:
      - {name: enter, match: ^POST /enter$}
      - {name: post, match: ^POST /comment$}
`))
	r := func(e string, dt float64, m, ep string, st int) Request {
		return Request{T: origin + dt, Method: m, Endpoint: ep, URI: ep + "?x=1", Host: "a.example", Entity: e, Status: st, ReqTime: 0.1}
	}
	return cfg, &Input{GroupId: "G", Origin: origin, Requests: []Request{
		// cookie-a: 入室して投稿まで進む
		r("cookie-a", 2, "GET", "/top", 200),
		r("cookie-a", 3, "POST", "/enter", 200),
		r("cookie-a", 4, "POST", "/comment", 201),
		r("cookie-a", 5, "POST", "/comment", 499),
		// cookie-b: 入室だけ
		r("cookie-b", 1, "GET", "/top", 200),
		r("cookie-b", 6, "POST", "/enter", 500),
		// cookie-c: ライフサイクルに入らない
		r("cookie-c", 7, "GET", "/top", 200),
		// 識別子なしは無視
		r("", 1, "GET", "/top", 200),
	}}
}

func TestSessionListAndFilter(t *testing.T) {
	cfg, in := sessionInput()
	idx := buildSessionIndex(cfg, in)

	all := idx.list("")
	if all.Total != 3 || len(all.Sessions) != 3 {
		t.Fatalf("all = %+v", all)
	}
	// 開始時刻順: b(1s), a(2s), c(7s)
	b, a := all.Sessions[0], all.Sessions[1]
	if b.StartSec != 1 || a.StartSec != 2 || a.Requests != 4 || a.S499 != 1 || b.Errors != 1 {
		t.Errorf("sessions = %+v", all.Sessions)
	}
	if a.Lifecycles[0].Reached != 2 || a.Lifecycles[0].LastStep != "post" || b.Lifecycles[0].LastStep != "enter" {
		t.Errorf("reach = %+v / %+v", a.Lifecycles, b.Lifecycles)
	}
	// ID は開始時刻順の連番
	if b.ID != "s001" || a.ID != "s002" || all.Sessions[2].ID != "s003" {
		t.Errorf("ids = %s %s %s", b.ID, a.ID, all.Sessions[2].ID)
	}
	if strings.Join(a.Hosts, ",") != "a.example" {
		t.Errorf("hosts = %v", a.Hosts)
	}

	if v := idx.list("viewer"); len(v.Sessions) != 2 || v.Total != 3 {
		t.Errorf("viewer = %+v", v)
	}
}

func TestSessionDetailMarksAdvancedSteps(t *testing.T) {
	cfg, in := sessionInput()
	idx := buildSessionIndex(cfg, in)
	d, ok := idx.detail("s002") // cookie-a
	if !ok || len(d.Requests) != 4 {
		t.Fatalf("detail = %+v", d)
	}
	steps := []string{}
	for _, r := range d.Requests {
		steps = append(steps, strings.Join(r.Steps, "+"))
	}
	// 2回目の投稿は状態を進めない
	if got := strings.Join(steps, ","); got != ",viewer:enter,viewer:post," {
		t.Errorf("steps = %s", got)
	}
	if d.Requests[1].URI != "/enter?x=1" || d.Requests[1].Host != "a.example" || d.Requests[1].OffsetSec != 3 {
		t.Errorf("request = %+v", d.Requests[1])
	}
	if _, ok := idx.detail("nope"); ok {
		t.Error("unknown session must not be found")
	}
}

func TestTransitions(t *testing.T) {
	cfg, in := sessionInput()
	idx := buildSessionIndex(cfg, in)

	tr := idx.transitions("viewer", 0)
	if tr.Sessions != 2 {
		t.Fatalf("sessions = %d", tr.Sessions)
	}
	find := func(from, to string) *Transition {
		for i := range tr.Transitions {
			if tr.Transitions[i].From == from && tr.Transitions[i].To == to {
				return &tr.Transitions[i]
			}
		}
		return nil
	}
	// 両方とも GET /top で始まり、/top の次は /enter（a は 1s 後、b は 5s 後）
	if x := find(transitionStart, "GET /top"); x == nil || x.Count != 2 || x.ShareOfFrom != 1 {
		t.Errorf("start = %+v", x)
	}
	if x := find("GET /top", "POST /enter"); x == nil || x.Count != 2 || x.GapP50Sec != 1 {
		t.Errorf("top->enter = %+v", x)
	}
	// /comment の次: もう一度 /comment が1回、終了が1回
	if x := find("POST /comment", "POST /comment"); x == nil || x.ShareOfFrom != 0.5 {
		t.Errorf("comment->comment = %+v", x)
	}
	if x := find("POST /enter", transitionEnd); x == nil || x.Count != 1 {
		t.Errorf("enter->end = %+v", x)
	}
	if all := idx.transitions("", 0); all.Sessions != 3 {
		t.Errorf("all sessions = %d", all.Sessions)
	}
}
