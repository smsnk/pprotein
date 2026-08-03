package score

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/smsnk/pprotein/internal/collect"
)

type processor struct{}

func (p *processor) Cacheable() bool {
	return false
}

// Process は保存した JSON をそのまま返す。外部プロセスは要らない。
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
