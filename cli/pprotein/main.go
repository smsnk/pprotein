package main

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/integration/echov4"
	"github.com/smsnk/pprotein/internal/collect"
	"github.com/smsnk/pprotein/internal/collect/group"
	"github.com/smsnk/pprotein/internal/event"
	"github.com/smsnk/pprotein/internal/extproc/alp"
	"github.com/smsnk/pprotein/internal/extproc/slp"
	"github.com/smsnk/pprotein/internal/memo"
	"github.com/smsnk/pprotein/internal/pprof"
	"github.com/smsnk/pprotein/internal/storage"
	"github.com/smsnk/pprotein/internal/top"
	"github.com/smsnk/pprotein/view"
)

func start() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}

	store, err := storage.New("data")
	if err != nil {
		return err
	}

	e := echo.New()
	echov4.Integrate(e)

	fs, err := view.FS()
	if err != nil {
		return err
	}
	e.GET("/*", echo.WrapHandler(http.FileServer(http.FS(fs))))

	api := e.Group("/api", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "no-store")
			return next(c)
		}
	})

	hub := event.NewHub()
	hub.RegisterHandlers(api.Group("/event"))

	pprofOpts := &collect.Options{
		Type:     "pprof",
		Ext:      "-pprof.pb.gz",
		Store:    store,
		EventHub: hub,
	}
	if err := pprof.NewHandler(pprofOpts).Register(api.Group("/pprof")); err != nil {
		return err
	}

	topOpts := &collect.Options{
		Type:     "top",
		Ext:      "-toplog.json",
		Store:    store,
		EventHub: hub,
	}
	if err := top.NewHandler(topOpts).Register(api.Group("/top")); err != nil {
		return err
	}

	alpOpts := &collect.Options{
		Type:     "httplog",
		Ext:      "-httplog.log",
		Store:    store,
		EventHub: hub,
	}
	alpHandler, err := alp.NewHandler(alpOpts, store)
	if err != nil {
		return err
	}
	if err := alpHandler.Register(api.Group("/httplog")); err != nil {
		return err
	}

	slpOpts := &collect.Options{
		Type:     "slowlog",
		Ext:      "-slowlog.log",
		Store:    store,
		EventHub: hub,
	}
	slpHandler, err := slp.NewHandler(slpOpts, store)
	if err != nil {
		return err
	}
	if err := slpHandler.Register(api.Group("/slowlog")); err != nil {
		return err
	}

	memoOpts := &collect.Options{
		Type:     "memo",
		Ext:      "-memo.log",
		Store:    store,
		EventHub: hub,
	}
	if err := memo.NewHandler(memoOpts).Register(api.Group("/memo")); err != nil {
		return err
	}

	grp, err := group.NewCollector(store, port)
	if err != nil {
		return err
	}
	grp.RegisterHandlers(api.Group("/group"))

	return e.Start(":" + port)
}

func main() {
	if err := start(); err != nil {
		panic(err)
	}
}
