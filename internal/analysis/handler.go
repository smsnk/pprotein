package analysis

import (
	_ "embed"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/persistent"
	"github.com/smsnk/pprotein/internal/storage"
	"github.com/smsnk/pprotein/internal/summary"
)

//go:embed analysis.yml
var defaultConfig []byte

// Handler は /api/analysis を提供する。
// trend と同じく自分自身の API から材料を読むので、収集タイプの実装には手を入れない。
type Handler struct {
	client *summary.Client
	config *persistent.Handler
}

func NewHandler(selfURL string, store storage.Storage) (*Handler, error) {
	config, err := persistent.New(store, "analysis.yml", defaultConfig, sanitize)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	return &Handler{client: summary.NewClient(selfURL), config: config}, nil
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
	g.GET("/:id", h.get)
}

// get は GET /api/analysis/:id。id に latest を渡すと httplog がある最新の収集を使う。
func (h *Handler) get(c echo.Context) error {
	raw, err := h.config.GetContent()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	cfg, err := ParseConfig(raw)
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

	in, err := Load(h.client, cfg, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	in.ConfigHash.Analysis = dbquery.HashConfig(raw)
	return c.JSON(http.StatusOK, Analyze(cfg, in))
}

// LatestGroup は httplog がある最新の収集グループを返す。
// 収集中（pending）のものも含める。走行直後に latest を引いて1つ前の走行が出ると、取り違えに気づけない。
func LatestGroup(client *summary.Client) (string, error) {
	entries, err := client.Entries("httplog")
	if err != nil {
		return "", err
	}
	live := []summary.Entry{}
	for _, e := range entries {
		if e.Status != "fail" {
			live = append(live, e)
		}
	}
	ids := summary.GroupIDs(live)
	if len(ids) == 0 {
		return "", fmt.Errorf("no httplog is collected")
	}
	return ids[0], nil
}

// Load は収集グループの httplog（生ログ）と dbquery の結果を集める。
func Load(client *summary.Client, cfg *Config, groupId string) (*Input, error) {
	origin, err := time.ParseInLocation(dbquery.GroupIdLayout, groupId, time.Local)
	if err != nil {
		return nil, fmt.Errorf("GroupId is not a time: %w", err)
	}
	in := &Input{
		GroupId: groupId,
		Origin:  float64(origin.UnixNano()) / 1e9,
		Queries: map[string]*dbquery.QueryResult{},
	}

	alpConfig, err := client.Config("httplog")
	if err != nil {
		return nil, fmt.Errorf("failed to read alp config: %w", err)
	}
	norm, err := NewNormalizer(alpConfig)
	if err != nil {
		return nil, err
	}

	found := false
	httplogs, err := client.Entries("httplog")
	if err != nil {
		return nil, err
	}
	in.Warnings = append(in.Warnings, incomplete("httplog", httplogs, groupId, cfg.HTTPLog.Label, false)...)
	for _, e := range httplogs {
		if e.Snapshot.GroupId != groupId || e.Status != "ok" {
			continue
		}
		if cfg.HTTPLog.Label != "" && e.Snapshot.Label != cfg.HTTPLog.Label {
			continue
		}
		if in.Commit == nil && e.Snapshot.Repository != nil && e.Snapshot.Repository.Hash != "" {
			r := e.Snapshot.Repository
			in.Commit = &Commit{Hash: r.Hash, Message: firstLine(r.Message), Ref: r.Ref}
		}
		data, err := client.RawData("httplog", e.Snapshot.ID)
		if err != nil {
			return nil, err
		}
		reqs, skipped := ParseLTSV(data, norm, cfg.HTTPLog.TimeKey, cfg.HTTPLog.EntityKey)
		in.Requests = append(in.Requests, reqs...)
		in.SkippedLines += skipped
		found = true
	}

	// dbquery は無くてもよい（古い収集や、SQL を使わない回）。
	if dbq, err := client.Entries("dbquery"); err == nil {
		var latest *summary.Entry
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
		in.Warnings = append(in.Warnings, incomplete("dbquery", dbq, groupId, "", latest != nil)...)
		if latest != nil {
			body, err := client.Content("dbquery", latest.Snapshot.ID)
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
	if scores, err := client.Entries("score"); err == nil {
		var latest *summary.Entry
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
			if v, err := client.Score(latest.Snapshot.ID); err == nil {
				in.Score = &ScoreInfo{Value: v.Score, Passed: v.Passed}
			}
		}
	}

	// 収集中・失敗しか無いときは、空の集計に警告を付けて返す（エラーの本文は CLI で切り詰められるため）。
	if !found && len(in.Warnings) == 0 {
		return nil, fmt.Errorf("group %s has neither httplog nor dbquery", groupId)
	}
	return in, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// incomplete は収集グループのうち確定していない（pending / fail の）エントリを警告文にする。
// 黙って飛ばすと、収集中の走行がライフサイクル 0 件・リクエスト 0 件として読まれてしまう。
func incomplete(typ string, entries []summary.Entry, groupId, label string, ignoreFail bool) []string {
	warnings := []string{}
	for _, e := range entries {
		if e.Snapshot.GroupId != groupId {
			continue
		}
		if label != "" && e.Snapshot.Label != label {
			continue
		}
		name := typ
		if e.Snapshot.Label != "" {
			name += " " + e.Snapshot.Label
		}
		switch e.Status {
		case "pending":
			warnings = append(warnings, fmt.Sprintf("%s は収集中（pending）。この集計に含めていない。完了してから再実行する", name))
		case "fail":
			if !ignoreFail {
				warnings = append(warnings, fmt.Sprintf("%s は収集に失敗した（%s）。この集計に含めていない", name, e.Message))
			}
		}
	}
	return warnings
}

// Pending は収集グループの httplog / dbquery のうち、まだ収集中のエントリの数を返す。
func Pending(client *summary.Client, groupId string) (int, error) {
	n := 0
	for _, typ := range []string{"httplog", "dbquery"} {
		entries, err := client.Entries(typ)
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
