package analysis

import (
	stdjson "encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/client"
	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/md"
	"github.com/smsnk/pprotein/internal/score"
)

// 複数の走行の要約を1回で返す（GET /api/analysis?limit=N）。
//
// 施策を重ねるごとに得点・母数・利用者の進み方がどう推移したかを、AI が1回の読み込みで掴めるようにする。
// 人向けのグラフもこれをデータ源にする。走行ごとの明細（時系列・内訳）は載せず、/api/analysis/:id で引く。

const (
	defaultRunsLimit = 20
	maxRunsLimit     = 100
	topEndpointsN    = 5
)

type (
	RunsResponse struct {
		// Runs は古い順（グラフの左から右）。
		Runs []RunSummary
	}

	RunSummary struct {
		GroupId    string
		Score      *score.Brief `json:",omitempty"`
		Commit     *git.Commit  `json:",omitempty"`
		ConfigHash ConfigHash
		// Warnings は収集中・失敗のエントリなど、この走行の集計が欠けている理由。
		Warnings []string `json:",omitempty"`

		Requests   RequestSummary
		Series     []SeriesStat
		Lifecycles []LifecycleSummary
		// TopEndpoints は件数の多いエンドポイント（構成の変化を追う）。
		TopEndpoints []EndpointShare
		// Endpoints は全エンドポイントの集計。?endpoints=1 のときだけ入る（走行数 × エンドポイント数で大きくなるため）。
		Endpoints []EndpointStat `json:",omitempty"`
	}

	RequestSummary struct {
		Requests     int
		S4xx         int
		S5xx         int
		S499         int
		P50          float64 `json:"P50Sec"`
		P99          float64 `json:"P99Sec"`
		PeakRequests int
	}

	LifecycleSummary struct {
		Name     string
		Entities int
		// Reached は状態ごとの到達数（Steps の順）。
		Steps []StepReached
	}

	StepReached struct {
		Name    string
		Reached int
	}

	EndpointShare struct {
		Key   string
		Count int
		// Share は全リクエストに占める割合（0〜1）。
		Share float64
	}

	// memo は集計結果を覚えておく。生ログを読み直すのが重いので、
	// 同じ材料・同じ設定なら使い回す。材料が確定していない（収集中の）走行は覚えない。
	memo[T any] struct {
		mu    sync.Mutex
		data  map[string]T
		order []string // 入れた順。上限を超えたら古いものから捨てる
	}
)

// memoLimit はキャッシュに置く走行の数。本番の httplog は素振りより桁違いに大きく、
// 上限が無いと競技の途中で observer のメモリが尽きて pprotein が落ちる。
const memoLimit = 30

// compactJSON はインデントせずに返す。echo の c.JSON は Debug 時にインデントし、
// AI が読むときのトークンが倍以上になる（ISUCON13 の1走行で 18KB → 7.5KB）。
func compactJSON(c echo.Context, v any) error {
	buf, err := stdjson.Marshal(v)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSONBlob(http.StatusOK, buf)
}

func newMemo[T any]() *memo[T] {
	return &memo[T]{data: map[string]T{}}
}

func (c *memo[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.data[key]
	return r, ok
}

func (c *memo[T]) put(key string, r T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.order = append(c.order, key)
	}
	c.data[key] = r
	for len(c.order) > memoLimit {
		delete(c.data, c.order[0])
		c.order = c.order[1:]
	}
}

// entryIndex は収集タイプをまたいだエントリを GroupId ごとにまとめる。
type entryIndex map[string][]client.Entry

// Types は analyze が読む収集タイプ。
var Types = []string{"httplog", "dbquery", "score"}

func loadEntryIndex(api *client.Client) (entryIndex, error) {
	idx := entryIndex{}
	for _, typ := range Types {
		entries, err := api.Entries(typ)
		if err != nil {
			if typ == "httplog" {
				return nil, err
			}
			continue // dbquery / score が無い pprotein でも動くように
		}
		for _, e := range entries {
			idx[e.Snapshot.GroupId] = append(idx[e.Snapshot.GroupId], e)
		}
	}
	return idx, nil
}

// fingerprint は走行の材料を表すキー。エントリの増減・状態の変化・設定の変更で変わる。
// 確定していない（pending の）エントリがあれば空を返し、キャッシュしない。
func (idx entryIndex) fingerprint(groupId, configHash string) string {
	parts := []string{groupId, configHash}
	for _, e := range idx[groupId] {
		if e.Status == "pending" {
			return ""
		}
		parts = append(parts, e.Snapshot.Type+":"+e.Snapshot.ID+":"+e.Status)
	}
	sort.Strings(parts[2:])
	return strings.Join(parts, "|")
}

// report は1走行の Report を返す。キャッシュにあればそれを使う。
func (h *Handler) report(cfg *Config, raw []byte, idx entryIndex, groupId string) (*Report, error) {
	hash := hashConfig(raw)
	key := idx.fingerprint(groupId, hash)
	if key != "" {
		if r, ok := h.cache.get(key); ok {
			return r, nil
		}
	}
	in, err := Load(h.client, cfg, groupId)
	if err != nil {
		return nil, err
	}
	in.ConfigHash.Analysis = hash
	rep := Analyze(cfg, in)
	if key != "" && len(in.Warnings) == 0 {
		h.cache.put(key, rep)
	}
	return rep, nil
}

// getRuns は GET /api/analysis?limit=N。
func (h *Handler) getRuns(c echo.Context) error {
	cfg, raw, err := h.loadConfig()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	limit := defaultRunsLimit
	if v, err := strconv.Atoi(c.QueryParam("limit")); err == nil && v > 0 {
		limit = min(v, maxRunsLimit)
	}

	withEndpoints := c.QueryParam("endpoints") == "1"

	idx, err := loadEntryIndex(h.client)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	httplogs := []client.Entry{}
	for _, entries := range idx {
		for _, e := range entries {
			if e.Snapshot.Type == "httplog" && e.Status != "fail" {
				httplogs = append(httplogs, e)
			}
		}
	}
	ids := client.GroupIDs(httplogs) // 新しい順
	if len(ids) > limit {
		ids = ids[:limit]
	}
	sort.Strings(ids)

	resp := &RunsResponse{Runs: []RunSummary{}}
	for _, id := range ids {
		rep, err := h.report(cfg, raw, idx, id)
		if err != nil {
			continue // GroupId が時刻でない・材料が無い走行は飛ばす
		}
		s := Summarize(rep)
		if withEndpoints {
			s.Endpoints = rep.EndpointStats
		}
		resp.Runs = append(resp.Runs, s)
	}
	return compactJSON(c, resp)
}

// getDiff は GET /api/analysis/diff?before=<GroupId>&after=<GroupId>[&top=N]。
// pprotein-cli analyze diff --json と同じものを返す（UI の比較画面が使う）。
func (h *Handler) getDiff(c echo.Context) error {
	before, after := c.QueryParam("before"), c.QueryParam("after")
	if before == "" || after == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "before and after are required")
	}
	top := defaultTop
	if v, err := strconv.Atoi(c.QueryParam("top")); err == nil && v > 0 {
		top = v
	}
	cfg, raw, err := h.loadConfig()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	idx, err := loadEntryIndex(h.client)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	b, err := h.report(cfg, raw, idx, before)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	a, err := h.report(cfg, raw, idx, after)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return compactJSON(c, Compare(b, a, top))
}

func hashConfig(raw []byte) string {
	return dbquery.HashConfig(raw)
}

// Summarize は Report から推移を見るのに要る値だけを抜き出す。
func Summarize(rep *Report) RunSummary {
	t := rep.Totals
	s := RunSummary{
		GroupId:    rep.GroupId,
		Score:      rep.Score,
		Commit:     rep.Commit,
		ConfigHash: rep.ConfigHash,
		Warnings:   rep.Warnings,
		Requests: RequestSummary{
			Requests: t.Requests, S4xx: t.S4xx, S5xx: t.S5xx, S499: t.Canceled,
			P50: t.P50, P99: t.P99, PeakRequests: t.PeakRequests,
		},
		Series: rep.SeriesStats,
	}
	for _, l := range rep.Lifecycles {
		ls := LifecycleSummary{Name: l.Name, Entities: l.Entities}
		for _, st := range l.Steps {
			ls.Steps = append(ls.Steps, StepReached{Name: st.Name, Reached: st.Reached})
		}
		s.Lifecycles = append(s.Lifecycles, ls)
	}
	total := sumCounts(t.Endpoints)
	for _, k := range topKeys(t.Endpoints, topEndpointsN) {
		share := 0.0
		if total > 0 {
			share = math.Round(float64(t.Endpoints[k])/float64(total)*1e4) / 1e4
		}
		s.TopEndpoints = append(s.TopEndpoints, EndpointShare{Key: k, Count: t.Endpoints[k], Share: share})
	}
	return s
}

// RenderRunsMarkdown は走行を1行ずつ並べた表にする。
// 系列は役割（score / driver / penalty）が付いたものを優先し、無ければ先頭から最大6本。
func RenderRunsMarkdown(w io.Writer, resp *RunsResponse) {
	fmt.Fprintf(w, "# analyze trend（%d 走行、古い順）\n\n", len(resp.Runs))
	if len(resp.Runs) == 0 {
		fmt.Fprintln(w, "走行なし")
		return
	}

	series := []string{}
	roles := map[string]string{}
	seen := map[string]bool{}
	for _, r := range resp.Runs {
		for _, st := range r.Series {
			if st.Role != "" && !seen[st.Name] {
				seen[st.Name] = true
				roles[st.Name] = st.Role
				series = append(series, st.Name)
			}
		}
	}
	sort.SliceStable(series, func(i, j int) bool { return roleOrder(roles[series[i]]) < roleOrder(roles[series[j]]) })
	if len(series) == 0 {
		for _, st := range resp.Runs[len(resp.Runs)-1].Series {
			if len(series) == 6 {
				break
			}
			series = append(series, st.Name)
		}
	}
	lifecycles := []string{}
	for _, l := range resp.Runs[len(resp.Runs)-1].Lifecycles {
		lifecycles = append(lifecycles, l.Name)
	}

	head := []string{"GroupId", "commit", "score"}
	for _, n := range series {
		if r := roles[n]; r != "" {
			n += " [" + r + "]"
		}
		head = append(head, n)
	}
	head = append(head, "req", "5xx", "499", "p99")
	for _, l := range lifecycles {
		head = append(head, l+" 開始→最後")
	}
	md.Row(w, head)
	md.Sep(w, len(head))

	for _, r := range resp.Runs {
		row := []string{r.GroupId, "", "-"}
		if r.Commit != nil {
			row[1] = r.Commit.ShortHash()
		}
		if r.Score != nil {
			row[2] = r.Score.String()
		}
		totals := map[string]float64{}
		for _, st := range r.Series {
			totals[st.Name] = st.Total
		}
		for _, n := range series {
			if v, ok := totals[n]; ok {
				row = append(row, md.Num(v))
			} else {
				row = append(row, "-")
			}
		}
		q := r.Requests
		row = append(row, md.Int(q.Requests), md.Int(q.S5xx), md.Int(q.S499), md.Sec(q.P99))
		for _, name := range lifecycles {
			cell := "-"
			for _, l := range r.Lifecycles {
				if l.Name == name && len(l.Steps) > 0 {
					cell = fmt.Sprintf("%d→%d", l.Entities, l.Steps[len(l.Steps)-1].Reached)
				}
			}
			row = append(row, cell)
		}
		if len(r.Warnings) > 0 {
			row[0] += " ⚠"
		}
		md.Row(w, row)
	}
	fmt.Fprintln(w)

	hashes := map[string]bool{}
	for _, r := range resp.Runs {
		hashes[r.ConfigHash.Analysis+"/"+r.ConfigHash.DBQuery] = true
	}
	if len(hashes) > 1 {
		md.Warnings(w, []string{"走行によって analysis.yml / dbquery.yml が違う。系列の値はその境目の前後で比べられないことがある"})
	}
	fmt.Fprintln(w, "- ⚠ の走行は集計が欠けている（収集中・失敗のエントリがある）。詳細は analyze <GroupId>")
}

func roleOrder(r string) int {
	switch r {
	case "score":
		return 0
	case "driver":
		return 1
	case "penalty":
		return 2
	}
	return 3
}
