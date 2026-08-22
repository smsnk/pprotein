// pprotein-cli は pprotein の計測結果をテキストで取り出す。
//
// UI は HTML なのでエージェントからは読めず、API は生 JSON で情報密度が悪い。
// 1コマンドの出力をそのまま文脈に貼れば、ボトルネックの議論が始められる状態にする。
//
//	pprotein-cli latest              直近の収集を要約する
//	pprotein-cli summary <groupId>   指定した収集を要約する
//	pprotein-cli groups              収集の一覧（スコア付き）
//	pprotein-cli diff <A> <B>        2つの収集を比較する
//	pprotein-cli collect             収集を開始して完了まで待ち、そのまま要約する
//
// 接続先は --url（既定 http://localhost:9000、環境変数 PPROTEIN_URL でも指定できる）。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/summary"
)

const usage = `pprotein-cli - pprotein の計測結果をテキストで取り出す

usage:
  pprotein-cli latest [flags]
  pprotein-cli summary <groupId> [flags]
  pprotein-cli groups [flags]
  pprotein-cli diff <before> <after> [flags]
  pprotein-cli collect [flags]

flags:
  --url string     pprotein の URL (既定 $PPROTEIN_URL または http://localhost:9000)
  --top int        各表に出す件数 (既定 10)
  --json           JSON で出力する
  --wait           collect: 収集の完了を待つ (既定 true)
  --timeout dur    collect: 完了待ちの上限 (既定 5m)
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return nil
	}
	cmd := os.Args[1]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	}

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.Usage = func() { fmt.Print(usage) }
	url := fs.String("url", envOr("PPROTEIN_URL", "http://localhost:9000"), "pprotein の URL")
	topN := fs.Int("top", 10, "各表に出す件数")
	asJSON := fs.Bool("json", false, "JSON で出力する")
	wait := fs.Bool("wait", true, "collect: 収集の完了を待つ")
	timeout := fs.Duration("timeout", 5*time.Minute, "collect: 完了待ちの上限")
	// flag は最初の非フラグ引数で解析を止めるので、
	// `diff <A> <B> --url ...` のようにフラグを後ろに置いても効くようにする。
	var args []string
	rest := os.Args[2:]
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		args = append(args, rest[0])
		rest = rest[1:]
	}

	c := summary.NewClient(*url)

	switch cmd {
	case "latest":
		return printGroup(c, "", *topN, *asJSON)

	case "summary":
		if len(args) < 1 {
			return fmt.Errorf("groupId を指定してください")
		}
		return printGroup(c, args[0], *topN, *asJSON)

	case "groups":
		return printGroups(c, *asJSON)

	case "diff":
		if len(args) < 2 {
			return fmt.Errorf("比較する2つの groupId を指定してください")
		}
		before, err := summary.Load(c, args[0], 0)
		if err != nil {
			return err
		}
		after, err := summary.Load(c, args[1], 0)
		if err != nil {
			return err
		}
		d := summary.Compare(before, after, *topN)
		if *asJSON {
			return printJSON(d)
		}
		fmt.Print(d.Markdown())
		return nil

	case "collect":
		return collect(c, *topN, *asJSON, *wait, *timeout)

	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func printGroup(c *summary.Client, id string, topN int, asJSON bool) error {
	g, err := summary.Load(c, id, topN)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(g)
	}
	fmt.Print(g.Markdown())
	return nil
}

func printGroups(c *summary.Client, asJSON bool) error {
	entries, err := c.AllEntries()
	if err != nil {
		return err
	}

	type row struct {
		GroupId string
		Score   string
		Commit  string
		Types   int
	}
	rows := []row{}
	for _, id := range summary.GroupIDs(entries) {
		r := row{GroupId: id}
		for _, e := range entries {
			if e.Snapshot.GroupId != id {
				continue
			}
			r.Types++
			if e.Snapshot.Type == "score" {
				r.Score = e.Message
			}
			if r.Commit == "" && e.Snapshot.Repository != nil && e.Snapshot.Repository.Hash != "" {
				h := e.Snapshot.Repository.Hash
				if len(h) > 7 {
					h = h[:7]
				}
				r.Commit = h
			}
		}
		rows = append(rows, r)
	}

	if asJSON {
		return printJSON(rows)
	}
	fmt.Printf("%-32s %-9s %-40s %s\n", "GROUP", "COMMIT", "SCORE", "ENTRIES")
	for _, r := range rows {
		fmt.Printf("%-32s %-9s %-40s %d\n", r.GroupId, r.Commit, r.Score, r.Types)
	}
	return nil
}

// collect は収集を開始し、新しい GroupId が現れて全エントリが確定するまで待つ。
// 「Duration 秒待ってからステータスを確認する」を手でやらずに済ませる。
func collect(c *summary.Client, topN int, asJSON, wait bool, timeout time.Duration) error {
	before, err := c.AllEntries()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, id := range summary.GroupIDs(before) {
		known[id] = true
	}

	if err := c.StartCollect(); err != nil {
		return fmt.Errorf("収集の開始に失敗しました: %w", err)
	}
	fmt.Fprintln(os.Stderr, "収集を開始しました")
	if !wait {
		return nil
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)

		entries, err := c.AllEntries()
		if err != nil {
			continue
		}
		newID := ""
		for _, id := range summary.GroupIDs(entries) {
			if !known[id] {
				newID = id
				break
			}
		}
		if newID == "" {
			continue
		}

		pending := 0
		for _, e := range entries {
			if e.Snapshot.GroupId == newID && e.Status == "pending" {
				pending++
			}
		}
		if pending > 0 {
			fmt.Fprintf(os.Stderr, "\r収集中 (%s): 残り %d 件 ", newID, pending)
			continue
		}

		fmt.Fprintf(os.Stderr, "\r収集が完了しました: %s\n\n", newID)
		return printGroup(c, newID, topN, asJSON)
	}
	return fmt.Errorf("収集が %s 以内に完了しませんでした", timeout)
}

func printJSON(v any) error {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(buf))
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
