// pprotein-cli は pprotein の計測結果をテキストで取り出す。
//
// UI は HTML なのでエージェントからは読めず、API は生 JSON で情報密度が悪い。
// 1コマンドの出力をそのまま文脈に貼れば、ボトルネックの議論が始められる状態にする。
// コマンドのリファレンスは docs/cli/ にある（pprotein-cli --help でも出る）。
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/client"
	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// options は全コマンドに共通のフラグ。
type options struct {
	url     string
	json    bool
	timeout time.Duration
}

func (o *options) client() *client.Client {
	c := client.New(o.url)
	c.HTTP.Timeout = o.timeout
	return c
}

func newRootCmd() *cobra.Command {
	o := &options{}
	root := &cobra.Command{
		Use:   "pprotein-cli",
		Short: "pprotein の計測結果をテキストで取り出す",
		Long: `pprotein の計測結果を、エージェントの文脈にそのまま貼れる Markdown（--json なら JSON）で出す。

系統は2つある。
  summary  サーバーの中で何が遅いか（alp / slp / pprof / resource の集計）
  analyze  得点がいつどこで生まれ、利用者がどこまで進んだか（httplog の生ログと dbquery）

GROUP は収集の GroupId（2026-10-17_10-41-02.984213 の形）。省略すると latest で、
収集中・失敗したものも含めた最新の収集を指す。`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetFlagErrorFunc(withUsage)

	f := root.PersistentFlags()
	f.StringVar(&o.url, "url", envOr("PPROTEIN_URL", "http://localhost:9000"), "pprotein の URL。環境変数 PPROTEIN_URL でも指定できる")
	f.BoolVar(&o.json, "json", false, "JSON で出力する")
	f.DurationVar(&o.timeout, "timeout", 5*time.Minute, "API の応答と、収集の完了待ちの上限")

	summary := newSummaryCmd(o)
	summary.AddCommand(newSummaryDiffCmd(o))
	analyze := newAnalyzeCmd(o)
	analyze.AddCommand(
		newAnalyzeDiffCmd(o), newAnalyzeTrendCmd(o), newEndpointsCmd(o),
		newSessionsCmd(o), newSessionCmd(o), newTransitionsCmd(o),
	)
	root.AddCommand(newCollectCmd(o), newGroupsCmd(o), summary, analyze)

	// 古い名前。使えるが help には出さない
	root.AddCommand(
		deprecated(newSummaryCmd(o), "latest", "summary"),
		deprecated(newSummaryDiffCmd(o), "diff", "summary diff"),
		deprecated(newAnalyzeDiffCmd(o), "analyze-diff", "analyze diff"),
		deprecated(newAnalyzeTrendCmd(o), "analyze-trend", "analyze trend"),
		deprecated(newEndpointsCmd(o), "endpoints", "analyze endpoints"),
		deprecated(newSessionsCmd(o), "sessions", "analyze sessions"),
		deprecated(newSessionCmd(o), "session", "analyze session"),
		deprecated(newTransitionsCmd(o), "transitions", "analyze transitions"),
	)
	return root
}

// deprecated は cmd を古い名前 name でも使えるようにする。help には出さず、使うと新しい名前を案内する。
// （cobra の Deprecated は、SetOut した出力先に英語の案内を混ぜる）
func deprecated(cmd *cobra.Command, name, use string) *cobra.Command {
	cmd.Use = name + strings.TrimPrefix(cmd.Use, cmd.Name())
	cmd.Hidden = true
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s は古い名前です。`pprotein-cli %s` を使ってください\n", name, use)
		return run(cmd, args)
	}
	return cmd
}

// withUsage はフラグや引数の誤りに、使い方の案内を足す。
func withUsage(cmd *cobra.Command, err error) error {
	return fmt.Errorf("%w（使い方は %s --help）", err, cmd.CommandPath())
}

// groupArgs は先頭の n 個の引数が GroupId か latest かを確かめる。
// summary / analyze はサブコマンドも持つので、打ち間違えたサブコマンドを GroupId として受け取らないようにする。
func groupArgs(n int, count cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := count(cmd, args); err != nil {
			return withUsage(cmd, err)
		}
		for _, arg := range args[:min(n, len(args))] {
			if arg == "latest" {
				continue
			}
			if _, err := time.Parse(dbquery.GroupIdLayout, arg); err == nil {
				continue
			}
			msg := fmt.Sprintf("%q は GroupId ではありません（%s の形か latest）", arg, dbquery.GroupIdLayout)
			if s := cmd.SuggestionsFor(arg); len(s) > 0 {
				msg += "。サブコマンドなら " + strings.Join(s, ", ")
			}
			return errors.New(msg)
		}
		return nil
	}
}

// groupArg は省略できる GROUP 引数を返す。
func groupArg(args []string) string {
	if len(args) == 0 {
		return "latest"
	}
	return args[0]
}

// resolve は latest を実際の GroupId にする。
func resolve(c *client.Client, id string) (string, error) {
	if id != "latest" {
		return id, nil
	}
	entries, err := c.AllEntries()
	if err != nil {
		return "", err
	}
	latest, err := client.Latest(entries)
	if err != nil {
		return "", fmt.Errorf("%w（まず pprotein-cli collect を実行してください）", err)
	}
	return latest, nil
}

// pollInterval は収集の完了を確かめる間隔。
var pollInterval = 3 * time.Second

// waitGroup は find が返す収集の types のエントリから pending が無くなるまで待ち、その GroupId を返す。
// types が空なら全部のタイプを待つ。find が空を返すあいだ（収集がまだ現れていない）も待つ。
func waitGroup(cmd *cobra.Command, c *client.Client, timeout time.Duration, find func([]client.Entry) string, types ...string) (string, error) {
	stderr := cmd.ErrOrStderr()
	deadline := time.Now().Add(timeout)
	failures := 0
	for {
		entries, err := c.AllEntries()
		if err != nil {
			// 一時的に読めないことはあるが、続くならサーバーが落ちている。待ち続けても意味がない
			if failures++; failures >= 3 || time.Now().After(deadline) {
				return "", fmt.Errorf("収集の状態を読めません: %w", err)
			}
		} else {
			failures = 0
			if id := find(entries); id != "" {
				pending := 0
				for _, e := range entries {
					if e.Snapshot.GroupId == id && e.Status == "pending" && (len(types) == 0 || slices.Contains(types, e.Snapshot.Type)) {
						pending++
					}
				}
				if pending == 0 {
					fmt.Fprintf(stderr, "\r収集が確定しました: %s\n\n", id)
					return id, nil
				}
				fmt.Fprintf(stderr, "\r収集中 (%s): 残り %d 件 ", id, pending)
			}
			if time.Now().After(deadline) {
				return "", fmt.Errorf("収集が %s 以内に確定しませんでした", timeout)
			}
		}
		time.Sleep(pollInterval)
	}
}

// output は --json なら v を JSON で、そうでなければ markdown を書く。
func (o *options) output(cmd *cobra.Command, v any, markdown func(w io.Writer)) error {
	w := cmd.OutOrStdout()
	if !o.json {
		markdown(w)
		return nil
	}
	// インデントしない（AI が読むときのトークンが倍以上になる）。SQL の < > & も \u003c にしない
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
