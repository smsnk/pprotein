package analysis

import (
	_ "embed"
	"fmt"
	"net/http"
	"time"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/client"
	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/persistent"
	"github.com/smsnk/pprotein/internal/storage"
)

//go:embed analysis.yml
var defaultConfig []byte

// Handler は /api/analysis を提供する。
// trend と同じく自分自身の API から材料を読むので、収集タイプの実装には手を入れない。
type Handler struct {
	client *client.Client
	config *persistent.Handler
	cache  *memo[*Report]
	// sessions はセッションの導線（sessions.go）の集計結果
	sessions *memo[*sessionIndex]
	// endpoints はエンドポイント別の区間ごとの集計（endpoints.go）
	endpoints *memo[*EndpointTimeline]
}

func NewHandler(selfURL string, store storage.Storage) (*Handler, error) {
	config, err := persistent.New(store, "analysis.yml", defaultConfig, sanitize)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	return &Handler{client: client.New(selfURL), config: config, cache: newMemo[*Report](), sessions: newMemo[*sessionIndex](), endpoints: newMemo[*EndpointTimeline]()}, nil
}

func sanitize(raw []byte) ([]byte, error) {
	// 検証だけして、書いたままの形（コメント込み）で保存する。
	if _, err := ParseConfig(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (h *Handler) RegisterHandlers(g *echo.Group) {
	h.config.RegisterHandlers(g.Group("/config"))
	g.GET("", h.getRuns)
	g.GET("/diff", h.getDiff)
	g.GET("/:id", h.get)
	g.GET("/:id/sessions", h.getSessions)
	g.GET("/:id/sessions/:sid", h.getSession)
	g.GET("/:id/transitions", h.getTransitions)
	g.GET("/:id/endpoints", h.getEndpoints)
}

func (h *Handler) loadConfig() (*Config, []byte, error) {
	raw, err := h.config.GetContent()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		return nil, nil, err
	}
	return cfg, raw, nil
}

// get は GET /api/analysis/:id。id に latest を渡すと httplog がある最新の収集を使う。
func (h *Handler) get(c echo.Context) error {
	cfg, raw, err := h.loadConfig()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	id := c.Param("id")
	if id == "latest" {
		id, err = LatestGroup(h.client)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
	}

	idx, err := loadEntryIndex(h.client)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	rep, err := h.report(cfg, raw, idx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return compactJSON(c, rep)
}

// LatestGroup は httplog がある最新の収集グループを返す。
// 収集中（pending）のものも含める。走行直後に latest を引いて1つ前の走行が出ると、取り違えに気づけない。
func LatestGroup(api *client.Client) (string, error) {
	entries, err := api.Entries("httplog")
	if err != nil {
		return "", err
	}
	live := []client.Entry{}
	for _, e := range entries {
		if e.Status != "fail" {
			live = append(live, e)
		}
	}
	ids := client.GroupIDs(live)
	if len(ids) == 0 {
		return "", fmt.Errorf("no httplog is collected")
	}
	return ids[0], nil
}

// Load は収集グループの httplog（生ログ）と dbquery の結果を集める。
func Load(api *client.Client, cfg *Config, groupId string) (*Input, error) {
	origin, err := time.ParseInLocation(dbquery.GroupIdLayout, groupId, time.Local)
	if err != nil {
		return nil, fmt.Errorf("GroupId is not a time: %w", err)
	}
	in := &Input{
		GroupId: groupId,
		Origin:  float64(origin.UnixNano()) / 1e9,
		Queries: map[string]*dbquery.QueryResult{},
	}

	alpConfig, err := api.Config("httplog")
	if err != nil {
		return nil, fmt.Errorf("failed to read alp config: %w", err)
	}
	norm, err := NewNormalizer(alpConfig)
	if err != nil {
		return nil, err
	}

	found := false
	httplogs, err := api.Entries("httplog")
	if err != nil {
		return nil, err
	}
	in.Warnings = append(in.Warnings, incomplete(httplogs, groupId, cfg.HTTPLog.Label, false)...)
	for _, e := range httplogs {
		if e.Snapshot.GroupId != groupId || e.Status != "ok" {
			continue
		}
		if cfg.HTTPLog.Label != "" && e.Snapshot.Label != cfg.HTTPLog.Label {
			continue
		}
		if in.Commit == nil {
			in.Commit = e.Snapshot.Repository.Commit()
		}
		data, err := api.RawData("httplog", e.Snapshot.ID)
		if err != nil {
			return nil, err
		}
		reqs, skipped := ParseLTSV(data, norm, cfg.HTTPLog.TimeKey, cfg.HTTPLog.EntityKey)
		in.Requests = append(in.Requests, reqs...)
		in.SkippedLines += skipped
		found = true
	}

	// dbquery は無くてもよい（古い収集や、SQL を使わない回）。
	if dbq, err := api.Entries("dbquery"); err == nil {
		var latest *client.Entry
		for i := range dbq {
			e := &dbq[i]
			if e.Snapshot.GroupId != groupId || e.Status != "ok" {
				continue
			}
			if latest == nil || e.Snapshot.Datetime.After(latest.Snapshot.Datetime) {
				latest = e
			}
		}
		// dbquery は手で再実行できるので、成功したものがあれば失敗の跡は無視する。
		in.Warnings = append(in.Warnings, incomplete(dbq, groupId, "", latest != nil)...)
		if latest != nil {
			body, err := api.Content("dbquery", latest.Snapshot.ID)
			if err != nil {
				return nil, err
			}
			res := &dbquery.Result{}
			if err := json.Unmarshal([]byte(body), res); err != nil {
				return nil, fmt.Errorf("failed to parse dbquery result: %w", err)
			}
			for i := range res.Queries {
				in.Queries[res.Queries[i].Name] = &res.Queries[i]
			}
			in.ConfigHash.DBQuery = res.ConfigHash
			found = true
		}
	}

	// スコアは無くてもよい（ベンチの結果を送っていない走行）。複数あれば新しいものを使う。
	if scores, err := api.Entries("score"); err == nil {
		var latest *client.Entry
		for i := range scores {
			e := &scores[i]
			if e.Snapshot.GroupId != groupId || e.Status != "ok" {
				continue
			}
			if latest == nil || e.Snapshot.Datetime.After(latest.Snapshot.Datetime) {
				latest = e
			}
		}
		if latest != nil {
			if v, err := api.Score(latest.Snapshot.ID); err == nil {
				in.Score = v.Brief()
			}
		}
	}

	// 収集中・失敗しか無いときは、空の集計に警告を付けて返す（エラーの本文は CLI で切り詰められるため）。
	if !found && len(in.Warnings) == 0 {
		return nil, fmt.Errorf("group %s has neither httplog nor dbquery", groupId)
	}
	return in, nil
}

// incomplete は収集グループのうち確定していない（pending / fail の）エントリを警告文にする。
func incomplete(entries []client.Entry, groupId, label string, ignoreFail bool) []string {
	warnings := []string{}
	for _, e := range entries {
		if e.Snapshot.GroupId != groupId || (label != "" && e.Snapshot.Label != label) {
			continue
		}
		if ignoreFail && e.Status == "fail" {
			continue
		}
		if w := client.Warning(e); w != "" {
			warnings = append(warnings, w)
		}
	}
	return warnings
}

// Pending は収集グループの httplog / dbquery のうち、まだ収集中のエントリの数を返す。
func Pending(api *client.Client, groupId string) (int, error) {
	n := 0
	for _, typ := range []string{"httplog", "dbquery"} {
		entries, err := api.Entries(typ)
		if err != nil {
			// dbquery は無くてもよい
			if typ == "dbquery" {
				continue
			}
			return 0, err
		}
		for _, e := range entries {
			if e.Snapshot.GroupId == groupId && e.Status == "pending" {
				n++
			}
		}
	}
	return n, nil
}
