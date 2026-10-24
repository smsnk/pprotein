package score

import (
	"testing"
	"time"
)

func TestBriefKeepsBenchDetails(t *testing.T) {
	start := time.Date(2025, 11, 23, 10, 41, 2, 0, time.UTC)
	v := &Value{Score: 18902, Passed: true, ErrorCount: 3, Target: "isu1", StartedAt: start, FinishedAt: start.Add(120 * time.Second), Raw: "{...}"}
	b := v.Brief()
	if got := b.Line(); got != "18902（エラー 3 / 対象 isu1 / 120s）" {
		t.Errorf("Line = %q", got)
	}
	// 詳細が無ければ String と同じ
	if got := (&Brief{Value: 0, Passed: false}).Line(); got != "0 (FAIL)" {
		t.Errorf("詳細が無ければ String と同じ: %q", got)
	}

	d := &Diff{Before: &Brief{Value: 12345, Passed: true, ErrorCount: 5}, After: b}
	if got := d.String(); got != "12345 -> 18902 (+6557, +53.1%) / エラー 5 -> 3" {
		t.Errorf("Diff = %q", got)
	}
	if got := (&Diff{Before: &Brief{Value: 1, Passed: true}, After: &Brief{Value: 2, Passed: true}}).String(); got != "1 -> 2 (+1, +100.0%)" {
		t.Errorf("エラーが無ければ書かない: %q", got)
	}
}
