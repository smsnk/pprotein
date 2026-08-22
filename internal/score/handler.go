package score

import (
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/collect"
)

type (
	// requestBody は POST /api/score のリクエスト。
	// GroupId には、この走行に対応する収集の GroupId を入れる。
	requestBody struct {
		GroupId string
		Label   string
		Value
	}

	handler struct {
		opts      *collect.Options
		collector *collect.Collector
	}
)

func NewHandler(opts *collect.Options) *handler {
	return &handler{opts: opts}
}

func (h *handler) Register(g *echo.Group) error {
	var err error
	h.collector, err = collect.New(&processor{}, h.opts)
	if err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	g.GET("", h.getIndex)
	g.POST("", h.postIndex)
	g.GET("/latest", h.getLatest)
	g.GET("/:id", h.getId)
	return nil
}

// getIndex は収集一覧に出すためのエントリを返す。
// Message にスコアの要約を入れて、一覧を見るだけでスコアが分かるようにする。
func (h *handler) getIndex(c echo.Context) error {
	list := h.collector.List()
	for _, entry := range list {
		if entry.Status != collect.StatusOk {
			continue
		}
		v, err := h.readValue(entry.Snapshot.ID)
		if err != nil {
			continue
		}
		entry.Message = v.Summary()
	}
	return c.JSON(http.StatusOK, list)
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
		req.Label = "bench"
	}

	buf, err := json.Marshal(&req.Value)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal score: %v", err))
	}

	snapshot, err := h.collector.Add(&collect.SnapshotTarget{
		GroupId: req.GroupId,
		Label:   req.Label,
	}, buf)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to add snapshot: %v", err))
	}

	eventData, err := json.Marshal(&collect.Entry{
		Snapshot: snapshot,
		Status:   collect.StatusOk,
		Message:  req.Value.Summary(),
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal entry: %v", err))
	}
	h.opts.EventHub.Publish(eventData)

	return c.JSON(http.StatusOK, echo.Map{"ID": snapshot.ID, "GroupId": req.GroupId})
}

// getLatest は最も新しいスコアを返す。CLI から「直近の走行のスコア」を引くのに使う。
func (h *handler) getLatest(c echo.Context) error {
	entries := h.collector.List()
	entries = slices.DeleteFunc(entries, func(e *collect.Entry) bool {
		return e.Status != collect.StatusOk
	})
	if len(entries) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no score is recorded")
	}
	slices.SortFunc(entries, func(a, b *collect.Entry) int {
		return b.Snapshot.Datetime.Compare(a.Snapshot.Datetime)
	})
	return h.writeValue(c, entries[0].Snapshot.ID)
}

func (h *handler) getId(c echo.Context) error {
	return h.writeValue(c, c.Param("id"))
}

func (h *handler) writeValue(c echo.Context, id string) error {
	r, err := h.collector.Get(id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("failed to get entry: %v", err))
	}
	defer r.Close()

	return c.Stream(http.StatusOK, echo.MIMEApplicationJSON, r)
}

func (h *handler) readValue(id string) (*Value, error) {
	r, err := h.collector.Get(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}
	defer r.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read entry: %w", err)
	}

	v := &Value{}
	if err := json.Unmarshal(buf, v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal score: %w", err)
	}
	return v, nil
}
