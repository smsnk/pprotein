package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra/doc"
)

var update = flag.Bool("update", false, "docs/cli/ を書き直す")

// docs/cli/ のリファレンスはコマンドの定義から作る。手で書くとコマンドとずれていく。
// 書き直すには make docs（go test ./cli/pprotein-cli -run TestReference -update）。
func TestReference(t *testing.T) {
	const dir = "../../docs/cli"
	t.Setenv("PPROTEIN_URL", "") // --url の既定値が環境で変わらないように

	got := t.TempDir()
	if err := doc.GenMarkdownTree(newRootCmd(), got); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.CopyFS(dir, os.DirFS(got)); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	files, _ := filepath.Glob(filepath.Join(got, "*.md"))
	if len(want) != len(files) {
		t.Errorf("docs/cli/ のファイル数が違う（%d 件、生成すると %d 件）。make docs で更新する", len(want), len(files))
	}
	for _, f := range files {
		name := filepath.Base(f)
		g, _ := os.ReadFile(f)
		w, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(g) != string(w) {
			t.Errorf("docs/cli/%s が古い。make docs で更新する", name)
		}
	}
}
