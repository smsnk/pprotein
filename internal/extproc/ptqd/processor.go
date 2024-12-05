package ptqd

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/smsnk/pprotein/internal/collect"
)

type (
	processor struct {
		confPath string
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

	cmd := exec.Command("pt-query-digest", "--config", p.confPath, bodyPath)

	res, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("external process aborted: %w", err)
	}

	return io.NopCloser(bytes.NewBuffer(res)), nil
}
