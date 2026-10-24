package md

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipKeepsUTF8(t *testing.T) {
	got := Clip(strings.Repeat("あ", 100), 160)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") || len(got) > 160+len("…") {
		t.Errorf("Clip = %q", got)
	}
	if got := Clip("short", 160); got != "short" {
		t.Errorf("短ければそのまま: %q", got)
	}
}

func TestRowEscapesCells(t *testing.T) {
	var b strings.Builder
	cells := []string{"a|b", "c\nd"}
	Row(&b, cells)
	if got := b.String(); got != "| a\\|b | c d |\n" {
		t.Errorf("Row = %q", got)
	}
	if cells[0] != "a|b" {
		t.Error("渡したセルを書き換えない")
	}
}
