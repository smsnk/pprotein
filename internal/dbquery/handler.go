package dbquery

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/collect"
	"github.com/smsnk/pprotein/internal/persistent"
	"github.com/smsnk/pprotein/internal/storage"
)

// GroupIdLayout は group.Collector が GroupId を作るときの書式。
const GroupIdLayout = "2006-01-02_15-04-05.999999"

type (
	// requestBody は POST /api/dbquery のリクエスト。
	// 走行が終わってから、その走行の GroupId を付けて叩く（次の initialize より前に）。
	requestBody struct {
		GroupId string
		Label   string
		// Since / Until は {{since}} / {{until}} に入る unix 秒。
		// 省略すると Since は GroupId の時刻（= 収集開始）、Until は現在時刻。
		Since int64
		Until int64
	}

	handler struct {
		opts      *collect.Options
		collector *collect.Collector
		config    *persistent.Handler
	}

	processor struct{}
)

//go:embed dbquery.yml
var defaultConfig []byte

func NewHandler(opts *collect.Options, store storage.Storage) (*handler, error) {
	config, err := persistent.New(store, "dbquery.yml", defaultConfig, sanitize)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	return &handler{opts: opts, config: config}, nil
}

// sanitize は検証だけして、書いたままの形（コメント込み）で保存する。
func sanitize(raw []byte) ([]byte, error) {
	if _, err := ParseConfig(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (h *handler) Register(g *echo.Group) error {
	var err error
	h.collector, err = collect.New(&processor{}, h.opts)
	if err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	h.config.RegisterHandlers(g.Group("/config"))
	g.GET("", h.getIndex)
	g.POST("", h.postIndex)
	g.GET("/:id", h.getId)
	g.GET("/data/:id", h.getId)
	return nil
}

func (h *handler) getIndex(c echo.Context) error {
	return c.JSON(http.StatusOK, h.collector.List())
}

func (h *handler) postIndex(c echo.Context) error {
	req := &requestBody{}
	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to parse request body: %v", err))
	}
	if req.GroupId == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "GroupId is required")
	}
	if req.Label == "" {
		req.Label = "db"
	}
	if req.Since == 0 {
		t, err := time.ParseInLocation(GroupIdLayout, req.GroupId, time.Local)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Since is omitted and GroupId is not a time: %v", err))
		}
		req.Since = t.Unix()
	}
	if req.Until == 0 {
		req.Until = time.Now().Unix()
	}

	raw, err := h.config.GetContent()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to read config: %v", err))
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	// 走行直後に同期で流す。呼び出し側（benchq）は返ってくるまで次の走行に進まない。
	res, err := Run(cfg, req.Since, req.Until)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, err.Error())
	}
	res.ConfigHash = HashConfig(raw)
	buf, err := json.Marshal(res)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal result: %v", err))
	}

	snapshot, err := h.collector.Add(&collect.SnapshotTarget{
		GroupId: req.GroupId,
		Label:   req.Label,
	}, buf)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to add snapshot: %v", err))
	}

	failed := []string{}
	for _, q := range res.Queries {
		if q.Error != "" {
			failed = append(failed, q.Name+": "+q.Error)
		}
	}
	return c.JSON(http.StatusOK, echo.Map{"ID": snapshot.ID, "GroupId": req.GroupId, "Failed": failed})
}

func (h *handler) getId(c echo.Context) error {
	r, err := h.collector.Get(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("failed to get entry: %v", err))
	}
	defer r.Close()
	return c.Stream(http.StatusOK, echo.MIMEApplicationJSON, r)
}

func (p *processor) Cacheable() bool {
	return false
}

func (p *processor) Process(snapshot *collect.Snapshot) (io.ReadCloser, error) {
	bodyPath, err := snapshot.BodyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find snapshot body: %w", err)
	}
	res, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot body: %w", err)
	}
	return io.NopCloser(bytes.NewBuffer(res)), nil
}
