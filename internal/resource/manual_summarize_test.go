package resource

import (
	"os"
	"testing"

	"github.com/goccy/go-json"
)

// 実機（Linux コンテナ）で取った Report を要約して目視するための補助。
//
//	RESOURCE_REPORT=/path/to/resource.json go test ./internal/resource/ -run TestManualSummarize -v
func TestManualSummarize(t *testing.T) {
	path := os.Getenv("RESOURCE_REPORT")
	if path == "" {
		t.Skip("RESOURCE_REPORT が未設定")
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := &Report{}
	if err := json.Unmarshal(buf, r); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + Summarize(r, 8).Text())
}
