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
