// Package trend は収集をまたいだ指標の推移を返す。
//
// pprotein の UI は1回の収集のスナップショット表示が中心で、走行をまたいだ比較ができない。
// 「前回と比べてどのエンドポイントが改善したか / 悪化したか」を人間が目視で突き合わせるのを
// やめるために、収集を横軸に取れる形のデータを1本の API で返す。
package trend

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/summary"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	defaultTopN  = 8
)

type (
	// Response は /api/trend の返り値。Groups は古い順（グラフの左から右）。
	Response struct {
		Groups []Point
	}

	// Point は1回の収集。
	Point struct {
		ID       string
		Datetime string

		Commit *Commit
		Score  *Score

		// Metrics はグラフに出す集計値。値が取れなかったものは入らない。
		Metrics map[string]float64

		// Endpoints / Queries はエンドポイント・クエリ単位の推移を引くための明細。
		Endpoints []Series
		Queries   []Series
	}

	Commit struct {
		Hash    string
		Message string
		Ref     string
	}

	Score struct {
		Value  int64
		Passed bool
	}

	// Series はエンドポイント / クエリ1件ぶんの値。Key で系列を束ねる。
	Series struct {
		Label string
		Key   string
		Count float64
		Sum   float64
		Avg   float64
	}
)

// 指標のキー。フロント側と合わせてある。
const (
	MetricHTTPSum    = "httplog.sum"
	MetricHTTPCount  = "httplog.count"
	MetricSlowSum    = "slowlog.sum"
	MetricSlowCount  = "slowlog.count"
	MetricSlowRows   = "slowlog.rows_examined"
	MetricScoreValue = "score"
)

// Handler は /api/trend を提供する。
// 集計は summary パッケージ（CLI と同じ経路）を使う。自分自身の API を叩くので、
// alp / pt-query-digest の結果はサーバー側のキャッシュがそのまま効く。
type Handler struct {
	client *summary.Client
}

func NewHandler(selfURL string) *Handler {
	return &Handler{client: summary.NewClient(selfURL)}
}

func (h *Handler) RegisterHandlers(g *echo.Group) {
	g.GET("", h.getTrend)
}

func (h *Handler) getTrend(c echo.Context) error {
	limit := intParam(c, "limit", defaultLimit, maxLimit)
	topN := intParam(c, "top", defaultTopN, 50)

	entries, err := h.client.AllEntries()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to list entries: %v", err))
	}

	ids := summary.GroupIDs(entries) // 新しい順
	if len(ids) > limit {
		ids = ids[:limit]
	}
	// グラフは古い順に並べる
	sort.Sort(sort.StringSlice(ids))

	resp := &Response{Groups: make([]Point, 0, len(ids))}
	for _, id := range ids {
		// topN で切り詰める前の全件を読む。Metrics（総数・合計）を
		// 上位 topN 件だけから集計すると、件数の多い軽量エンドポイントが
		// 抜け落ちて「リクエスト総数」が実際より小さく出る。
		g, err := summary.Load(h.client, id, 0)
		if err != nil {
			continue // 収集中 / 壊れている group は飛ばす
		}
		resp.Groups = append(resp.Groups, buildPoint(g, topN))
	}
	return c.JSON(http.StatusOK, resp)
}

func buildPoint(g *summary.Group, topN int) Point {
	p := Point{
		ID:      g.ID,
		Metrics: map[string]float64{},
	}
	if !g.Datetime.IsZero() {
		p.Datetime = g.Datetime.Format("2006-01-02 15:04:05")
	}
	if g.Repository != nil && g.Repository.Hash != "" {
		p.Commit = &Commit{
			Hash:    g.Repository.Hash,
			Message: g.Repository.Message,
			Ref:     g.Repository.Ref,
		}
	}
	if g.Score != nil {
		p.Score = &Score{Value: g.Score.Score, Passed: g.Score.Passed}
		p.Metrics[MetricScoreValue] = float64(g.Score.Score)
	}

	// httplog: 合計レスポンスタイムとリクエスト総数。
	// Metrics は収集全体の合計、明細（Endpoints / Queries）は上位 topN 件に絞る。
	var httpSum, httpCount float64
	for _, s := range g.HTTPLog {
		for _, e := range s.Endpoints {
			httpSum += e.Sum
			httpCount += e.Count
		}
		for _, e := range head(s.Endpoints, topN) {
			p.Endpoints = append(p.Endpoints, Series{
				Label: s.Label, Key: e.Method + " " + e.Uri,
				Count: e.Count, Sum: e.Sum, Avg: e.Avg,
			})
		}
	}
	if len(g.HTTPLog) > 0 {
		p.Metrics[MetricHTTPSum] = httpSum
		p.Metrics[MetricHTTPCount] = httpCount
	}

	var slowSum, slowCount, slowRows float64
	for _, s := range g.SlowLog {
		for _, q := range s.Queries {
			slowSum += q.Sum
			slowCount += q.Count
			slowRows += q.RowsExamined * q.Count
		}
		for _, q := range head(s.Queries, topN) {
			p.Queries = append(p.Queries, Series{
				Label: s.Label, Key: q.Query,
				Count: q.Count, Sum: q.Sum, Avg: q.Avg,
			})
		}
	}
	if len(g.SlowLog) > 0 {
		p.Metrics[MetricSlowSum] = slowSum
		p.Metrics[MetricSlowCount] = slowCount
		p.Metrics[MetricSlowRows] = slowRows
	}

	return p
}

// head は明細を上位 n 件に絞る（各リストは Sum 降順に並んでいる）。
func head[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}

func intParam(c echo.Context, key string, def, max int) int {
	v := c.QueryParam(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
