package pprof

import (
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/smsnk/pprotein/internal/collect"
)

type (
	handler struct {
		opts      *collect.Options
		collector *collect.Collector
	}
)

func NewHandler(opts *collect.Options) *handler {
	return &handler{opts: opts}
}

func (h *handler) Register(g *echo.Group) error {
	p := &processor{mu: &sync.Mutex{}, route: g}

	var err error
	h.collector, err = collect.New(p, h.opts)
	if err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	g.GET("", h.getIndex)
	g.POST("", h.postIndex)
	g.GET("/data/:id", h.getData)
	g.GET("/data/latest", h.getLatestData)

	return nil
}

func (h *handler) getIndex(c echo.Context) error {
	return c.JSON(http.StatusOK, h.collector.List())
}

func (h *handler) postIndex(c echo.Context) error {
	target := &collect.SnapshotTarget{}
	if err := c.Bind(target); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("failed to parse request body: %v", err))
	}

	go func() {
		if err := h.collector.Collect(target); err != nil {
			log.Error("[!] collector aborted:", err)
		}
	}()

	return c.NoContent(http.StatusOK)
}

func (h *handler) getData(c echo.Context) error {
	id := c.Param("id")
	entries := h.collector.List()
	for _, entry := range entries {
		if entry.Snapshot.ID == id {
			bodyPath, err := entry.Snapshot.BodyPath()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get body path: %v", err))
			}

			return c.File(bodyPath)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound)
}

func (h *handler) getLatestData(c echo.Context) error {
	label := c.QueryParam("label")

	entries := h.collector.List()
	slices.SortFunc(entries, func(a, b *collect.Entry) int {
		return b.Snapshot.SnapshotMeta.Datetime.Compare(a.Snapshot.SnapshotMeta.Datetime)
	})

	if len(entries) > 0 {
		for _, entry := range entries {
			if label != "" && label != entry.Snapshot.SnapshotTarget.Label {
				continue
			}
			if entry.Status != collect.StatusOk {
				continue
			}

			bodyPath, err := entry.Snapshot.BodyPath()
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get body path: %v", err))
			}
			return c.File(bodyPath)
		}
	}
	return echo.NewHTTPError(http.StatusNotFound)
}
