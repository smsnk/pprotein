// Package md は CLI が出す Markdown の部品。summary と analysis の表・数値の書き方を揃える。
package md

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Row は表の1行を書く。セルの中の | と改行は、表が崩れないように潰す。
func Row(w io.Writer, cells []string) {
	escaped := make([]string, len(cells))
	for i, c := range cells {
		c = strings.ReplaceAll(c, "|", `\|`)
		escaped[i] = strings.ReplaceAll(c, "\n", " ")
	}
	fmt.Fprintf(w, "| %s |\n", strings.Join(escaped, " | "))
}

// Sep は見出し行の下の区切りを書く。
func Sep(w io.Writer, n int) {
	fmt.Fprintf(w, "|%s\n", strings.Repeat(" --- |", n))
}

// Warnings は集計の前提が崩れていること（収集中・失敗など）を見出しの下に並べる。
func Warnings(w io.Writer, warnings []string) {
	for _, msg := range warnings {
		fmt.Fprintf(w, "- **警告**: %s\n", msg)
	}
}

// Num は件数や系列の値。整数ならそのまま、そうでなければ小数2桁。
func Num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return fmt.Sprintf("%.2f", v)
}

func Int(n int) string { return strconv.Itoa(n) }

// Sec は秒。ミリ秒まで出す。
func Sec(v float64) string { return fmt.Sprintf("%.3fs", v) }

// Pct は 0〜1 の割合を百分率にする。
func Pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

// Signed は f で書いた値の前に、正なら + を付ける。
func Signed(v float64, f func(float64) string) string {
	if v > 0 {
		return "+" + f(v)
	}
	return f(v)
}

// Change は before → after の変化率。before が 0 なら空。
func Change(before, after float64) string {
	if before == 0 {
		return ""
	}
	return fmt.Sprintf("%+.1f%%", (after-before)/math.Abs(before)*100)
}

// Clip は s を n バイト以内に切り詰める。途中で切れた最後の文字は落とす。
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
