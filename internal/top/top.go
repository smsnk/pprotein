package top

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/7crabs/sysmon-go/topfetcher"
)

type (
	TopHandler struct {
	}
)

func NewTopHandler() *TopHandler {
	return &TopHandler{}
}

func (h *TopHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.serve(w, r); err != nil {
		log.Printf("serve failed: %v", err)
	}
}

func (h *TopHandler) serve(w http.ResponseWriter, r *http.Request) error {
	seconds, err := strconv.Atoi(r.URL.Query().Get("seconds"))
	if err != nil {
		seconds = 30
	}

	var output io.Writer = w
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		ew, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
		if err != nil {
			return fmt.Errorf("failed to initialize gzip writer: %w", err)
		}
		defer ew.Close()

		output = ew
		w.Header().Set("Content-Encoding", "gzip")
	}

	data, err := topfetcher.FetchTopData(1, seconds)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		output.Write([]byte(err.Error()))
		return fmt.Errorf("failed to fetch top data: %w", err)
	}

	jsonData, err := topfetcher.ToJSON(data)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		output.Write([]byte(err.Error()))
		return fmt.Errorf("failed to convert data to JSON: %w", err)
	}

	// JSONデータを出力
	w.Header().Set("Content-Type", "application/json")
	_, err = output.Write([]byte(jsonData))
	if err != nil {
		return fmt.Errorf("failed to write JSON data: %w", err)
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
