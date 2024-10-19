package mux

import (
	"github.com/gorilla/mux"
	"github.com/smsnk/pprotein/integration"
)

func Integrate(r *mux.Router) {
	EnableDebugHandler(r)
	EnableDebugMode(r)
}

func EnableDebugHandler(r *mux.Router) {
	integration.RegisterDebugHandlers(r)
}

func EnableDebugMode(r *mux.Router) {
	return
}
