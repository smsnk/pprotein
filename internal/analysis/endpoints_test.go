package analysis

import "testing"

func endpointInput() (*Config, *Input) {
	cfg, _ := ParseConfig([]byte("bucket: 10\n"))
	r := func(dt float64, m, ep string, st int, rt float64) Request {
		return Request{T: origin + dt, Method: m, Endpoint: ep, Status: st, ReqTime: rt}
	}
	return cfg, &Input{GroupId: "G", Origin: origin, Requests: []Request{
		r(-1, "GET", "/a", 200, 9), // 収集開始より前は数えない
		r(1, "GET", "/a", 200, 0.1),
		r(2, "GET", "/a", 499, 20),
		r(3, "POST", "/b", 500, 0.3),
		r(25, "GET", "/a", 200, 0.2),
	}}
}

func TestEndpointStats(t *testing.T) {
	_, in := endpointInput()
	st := endpointStats(in)
	if len(st) != 2 || st[0].Key != "GET /a" || st[0].Count != 3 || st[0].Share != 0.75 {
		t.Fatalf("stats = %+v", st)
	}
	a, b := st[0], st[1]
	if a.S499 != 1 || a.Errors != 0 || a.P99 != 20 || a.Sum != 20.3 {
		t.Errorf("a = %+v", a)
	}
	if b.Errors != 1 || b.S499 != 0 {
		t.Errorf("b = %+v", b)
	}
}

func TestEndpointTimelineAlignsWithReportTimeline(t *testing.T) {
	cfg, in := endpointInput()
	tl := endpointTimeline(cfg, in)
	rep := Analyze(cfg, in)
	if len(tl.OffsetSec) != len(rep.Timeline.Buckets) {
		t.Fatalf("buckets: endpoints %v vs report %d", tl.OffsetSec, len(rep.Timeline.Buckets))
	}
	a := tl.Endpoints[0]
	if a.Key != "GET /a" || a.Count[0] != 2 || a.Count[1] != 0 || a.Count[2] != 1 || a.S499[0] != 1 || a.P99Sec[0] != 20 {
		t.Errorf("a = %+v", a)
	}
	if len(rep.EndpointStats) != 2 {
		t.Errorf("report endpoint stats = %+v", rep.EndpointStats)
	}
}
