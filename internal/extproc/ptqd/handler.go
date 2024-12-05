package ptqd

import (
	_ "embed"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/collect"
	"github.com/smsnk/pprotein/internal/extproc"
	"github.com/smsnk/pprotein/internal/persistent"
	"github.com/smsnk/pprotein/internal/storage"
)

type (
	handler struct {
		opts   *collect.Options
		config *persistent.Handler
	}
)

//go:embed ptqd.yml
var defaultConfig []byte

func NewHandler(opts *collect.Options, store storage.Storage) (*handler, error) {
	h := &handler{opts: opts}

	config, err := persistent.New(store, "ptqd.yml", defaultConfig, h.sanitize)
	if err != nil {
		return nil, fmt.Errorf("failed to create targets: %w", err)
	}
	h.config = config
	return h, nil
}

func (h *handler) Register(g *echo.Group) error {
	h.config.RegisterHandlers(g.Group("/config"))

	if err := extproc.NewHandler(&processor{confPath: h.config.GetPath()}, h.opts).Register(g); err != nil {
		return fmt.Errorf("failed to register extproc handlers: %w", err)
	}
	return nil
}

func (h *handler) sanitize(raw []byte) ([]byte, error) {
	return raw, nil
}
