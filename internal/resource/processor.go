package resource

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/collect"
)

// Processor は収集した Report を要約テキストに変換する（pprotein 本体側）。
// alp / pt-query-digest と違って外部コマンドを呼ばず、Go の中で集計する。
type Processor struct {
	// TopN は要約に出すプロセスの件数。0 なら DefaultTopN。
	TopN int
}

func (p *Processor) Cacheable() bool {
	return true
}

func (p *Processor) Process(snapshot *collect.Snapshot) (io.ReadCloser, error) {
	bodyPath, err := snapshot.BodyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to find snapshot body: %w", err)
	}

	buf, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot body: %w", err)
	}

	report := &Report{}
	if err := json.Unmarshal(buf, report); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report: %w", err)
	}

	text := Summarize(report, p.TopN).Text()
	return io.NopCloser(bytes.NewBufferString(text)), nil
}
