package top

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/smsnk/pprotein/internal/collect"
)

type (
	processor struct {
	}
)

func (p *processor) Cacheable() bool {
	return true
}

func (p *processor) Process(snapshot *collect.Snapshot) (io.ReadCloser, error) {
	bodyPath, err := snapshot.BodyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find snapshot body: %w", err)
	}

	content, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot body: %w", err)
	}

	return io.NopCloser(bytes.NewBuffer(content)), nil
}
