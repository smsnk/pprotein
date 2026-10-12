package analysis

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"

	"github.com/labstack/echo/v4"
)

// エンドポイント単位の集計。
//
// 時系列の Endpoints は JSON を小さく保つため上位 N 件しか持たない。ここでは全エンドポイントについて
// 走行全体の値（EndpointStat）と、区間ごとの値（EndpointTimeline）を出す。
// 「施策を入れたらこのエンドポイントの件数が増えた / 遅くなった」を、選んだエンドポイントで追えるようにする。

type (
	EndpointStat struct {
		Key   string
		Count int
		// Share は走行全体のリクエストに占める割合（0〜1）。
		Share float64
		P50   float64 `json:"P50Sec"`
		P99   float64 `json:"P99Sec"`
		// Sum は応答時間の合計（alp の SUM と同じもの）。
		Sum    float64 `json:"SumSec"`
		Errors int     // 4xx + 5xx（499 を除く）
		S499   int
	}

	EndpointTimeline struct {
		GroupId   string
		BucketSec int
		OffsetSec []int
		// Endpoints は件数の多い順。値の並びは OffsetSec と同じ。
		Endpoints []EndpointSeries
	}

	EndpointSeries struct {
		Key    string
		Count  []int
		P50Sec []float64
		P99Sec []float64
		Errors []int
		S499   []int
	}
)

// endpointStats は収集開始より後のリクエストをエンドポイントごとに集計する（件数の多い順）。
func endpointStats(in *Input) []EndpointStat {
	type acc struct {
		times  []float64
		sum    float64
		errors int
		s499   int
	}
	accs := map[string]*acc{}
	total := 0
	for i := range in.Requests {
		r := &in.Requests[i]
		if r.T < in.Origin {
			continue
		}
		a, ok := accs[r.Key()]
		if !ok {
			a = &acc{}
			accs[r.Key()] = a
		}
		a.times = append(a.times, r.ReqTime)
		a.sum += r.ReqTime
		switch {
		case r.Status == 499:
			a.s499++
		case r.Status >= 400:
			a.errors++
		}
		total++
	}
	out := make([]EndpointStat, 0, len(accs))
	for k, a := range accs {
		sort.Float64s(a.times)
		out = append(out, EndpointStat{
			Key:    k,
			Count:  len(a.times),
			Share:  math.Round(float64(len(a.times))/float64(total)*1e4) / 1e4,
			P50:    ms(quantile(a.times, 0.5)),
			P99:    ms(quantile(a.times, 0.99)),
			Sum:    ms(a.sum),
			Errors: a.errors,
			S499:   a.s499,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// endpointTimeline は全エンドポイントの区間ごとの件数・応答時間・エラーを出す。
// 区間の切り方は Report.Timeline と同じ（収集開始を 0 秒、Bucket 秒ごと）。
func endpointTimeline(cfg *Config, in *Input) *EndpointTimeline {
	type cell struct {
		times        []float64
		errors, s499 int
	}
	cells := map[string]map[int]*cell{}
	maxIdx := -1
	for i := range in.Requests {
		r := &in.Requests[i]
		off := r.T - in.Origin
		if off < 0 {
			continue
		}
		idx := int(off) / cfg.Bucket
		if idx > maxIdx {
			maxIdx = idx
		}
		m, ok := cells[r.Key()]
		if !ok {
			m = map[int]*cell{}
			cells[r.Key()] = m
		}
		c, ok := m[idx]
		if !ok {
			c = &cell{}
			m[idx] = c
		}
		c.times = append(c.times, r.ReqTime)
		switch {
		case r.Status == 499:
			c.s499++
		case r.Status >= 400:
			c.errors++
		}
	}

	tl := &EndpointTimeline{GroupId: in.GroupId, BucketSec: cfg.Bucket, OffsetSec: []int{}, Endpoints: []EndpointSeries{}}
	for i := 0; i <= maxIdx; i++ {
		tl.OffsetSec = append(tl.OffsetSec, i*cfg.Bucket)
	}
	for _, st := range endpointStats(in) {
		n := maxIdx + 1
		s := EndpointSeries{
			Key: st.Key, Count: make([]int, n), P50Sec: make([]float64, n), P99Sec: make([]float64, n),
			Errors: make([]int, n), S499: make([]int, n),
		}
		for idx, c := range cells[st.Key] {
			sort.Float64s(c.times)
			s.Count[idx] = len(c.times)
			s.P50Sec[idx] = ms(quantile(c.times, 0.5))
			s.P99Sec[idx] = ms(quantile(c.times, 0.99))
			s.Errors[idx], s.S499[idx] = c.errors, c.s499
		}
		tl.Endpoints = append(tl.Endpoints, s)
	}
	return tl
}

// getEndpoints は GET /api/analysis/:id/endpoints。
func (h *Handler) getEndpoints(c echo.Context) error {
	cfg, id, key, err := h.resolveRun(c)
	if err != nil {
		return err
	}
	if key != "" {
		if tl, ok := h.endpoints.get(key); ok {
			return compactJSON(c, tl)
		}
	}
	in, err := Load(h.client, cfg, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	tl := endpointTimeline(cfg, in)
	if key != "" && len(in.Warnings) == 0 {
		h.endpoints.put(key, tl)
	}
	return compactJSON(c, tl)
}

// RenderEndpointsMarkdown は走行全体のエンドポイント別の集計を表にする（CLI）。
func RenderEndpointsMarkdown(w io.Writer, groupId string, stats []EndpointStat) {
	fmt.Fprintf(w, "# endpoints %s（件数の多い順）\n\n", groupId)
	writeRow(w, []string{"エンドポイント", "件数", "構成比", "p50", "p99", "合計", "エラー", "499"})
	writeSep(w, 8)
	for _, s := range stats {
		writeRow(w, []string{s.Key, itoa(s.Count), pct(s.Share), sec(s.P50), sec(s.P99), sec(s.Sum), itoa(s.Errors), itoa(s.S499)})
	}
	fmt.Fprintln(w)
}
