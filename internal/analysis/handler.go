package analysis

import (
	_ "embed"
	"fmt"
	"net/http"
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
		id, err = latestGroup(h.client)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
	}

	in, err := Load(h.client, cfg, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return c.JSON(http.StatusOK, Analyze(cfg, in))
}

func latestGroup(client *summary.Client) (string, error) {
	entries, err := client.Entries("httplog")
	if err != nil {
		return "", err
	}
	ok := []summary.Entry{}
	for _, e := range entries {
		if e.Status == "ok" {
			ok = append(ok, e)
		}
	}
	ids := summary.GroupIDs(ok)
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
	for _, e := range httplogs {
		if e.Snapshot.GroupId != groupId || e.Status != "ok" {
			continue
		}
		if cfg.HTTPLog.Label != "" && e.Snapshot.Label != cfg.HTTPLog.Label {
			continue
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
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("group %s has neither httplog nor dbquery", groupId)
	}
	return in, nil
}
