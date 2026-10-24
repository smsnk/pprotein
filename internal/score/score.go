// Package score はベンチマークのスコアを収集グループ(GroupId)に紐づけて保存する。
//
// スコアはベンチ実行スクリプトの標準出力にしか無く、計測結果と機械的に繋がっていなかった。
// スコアを1つの snapshot として持たせることで、収集一覧・グラフ・CLI から
// 「この収集はスコアいくつの走行か」を引けるようにする。
//
// スコアの見方は回ごとに違うので、数値1本(Score)に加えてベンチの生出力(Raw)も保持する。
package score

import (
	"fmt"
	"strings"
	"time"

	"github.com/smsnk/pprotein/internal/md"
)

// Value は1回の走行の結果。snapshot の本体としてこの JSON が保存される。
type Value struct {
	// Score はスコア。回によらず「大きいほど良い」1本の数値。
	Score int64
	// Passed はベンチが成功したか。整合性チェック落ちなど、失敗した走行も記録する。
	Passed bool
	// Target は走行の対象（ホスト名など）。
	Target string
	// ErrorCount はベンチが報告したエラー件数。
	ErrorCount int
	// StartedAt / FinishedAt は走行区間。他の計測と突き合わせるのに使う。
	StartedAt  time.Time
	FinishedAt time.Time
	// Raw はベンチの生出力。回ごとに違う減点内訳などを後から読めるようにしておく。
	Raw string
}

// Summary は収集一覧やイベントに出す1行の要約。
func (v *Value) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "score=%d", v.Score)
	if v.Passed {
		b.WriteString(" pass")
	} else {
		b.WriteString(" FAIL")
	}
	if v.ErrorCount > 0 {
		fmt.Fprintf(&b, " errors=%d", v.ErrorCount)
	}
	if v.Target != "" {
		fmt.Fprintf(&b, " target=%s", v.Target)
	}
	if d := v.Duration(); d > 0 {
		fmt.Fprintf(&b, " %ds", int(d.Seconds()))
	}
	return b.String()
}

// Duration は走行時間。開始か終了が空なら 0。
func (v *Value) Duration() time.Duration {
	if v.StartedAt.IsZero() || v.FinishedAt.IsZero() {
		return 0
	}
	return v.FinishedAt.Sub(v.StartedAt)
}

// Brief は推移や比較に出すスコアの要点。
type Brief struct {
	Value  int64
	Passed bool
}

func (v *Value) Brief() *Brief {
	return &Brief{Value: v.Score, Passed: v.Passed}
}

// String は「18902」、失敗した走行なら「18902 (FAIL)」。
func (b *Brief) String() string {
	if b == nil {
		return "（なし）"
	}
	if !b.Passed {
		return fmt.Sprintf("%d (FAIL)", b.Value)
	}
	return fmt.Sprintf("%d", b.Value)
}

// Diff は2回の走行のスコア。片方に無ければ nil。
type Diff struct {
	Before *Brief `json:",omitempty"`
	After  *Brief `json:",omitempty"`
}

// String は「12345 -> 18902 (+6557, +53.1%)」。
func (d *Diff) String() string {
	before, after := d.Before, d.After
	line := before.String() + " -> " + after.String()
	if before == nil || after == nil {
		return line
	}
	b, a := float64(before.Value), float64(after.Value)
	delta := md.Signed(a-b, md.Num)
	if pct := md.Change(b, a); pct != "" {
		delta += ", " + pct
	}
	return line + " (" + delta + ")"
}
