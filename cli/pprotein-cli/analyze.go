package main

import (
	"fmt"
	"io"
	neturl "net/url"
	"strconv"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/analysis"
	"github.com/smsnk/pprotein/internal/client"
	"github.com/spf13/cobra"
)

// analyze 系は集計をサーバー側（/api/analysis）で行う。生ログを手元に持ってこない。

func newAnalyzeCmd(o *options) *cobra.Command {
	var wait bool
	cmd := &cobra.Command{
		Use:   "analyze [GROUP]",
		Short: "1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）",
		Args:  groupArgs(1, cobra.MaximumNArgs(1)),
		// 打ち間違えたサブコマンドを案内する（groupArgs）
		SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, groupArg(args))
			if err != nil {
				return err
			}
			// 走行直後は httplog がまだ収集中のことが多い。そのまま読むと空の集計になる
			if wait {
				if _, err := waitGroup(cmd, c, o.timeout, func([]client.Entry) string { return id }, analysis.Types...); err != nil {
					return err
				}
			}
			rep, err := fetchAnalysis(c, id)
			if err != nil {
				return err
			}
			return o.output(cmd, rep, func(w io.Writer) { analysis.RenderMarkdown(w, rep) })
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "その収集のうち analyze が読むエントリ（httplog / dbquery / score）が確定するまで待ってから出す（上限は --timeout）")
	return cmd
}

func fetchAnalysis(c *client.Client, id string) (*analysis.Report, error) {
	rep := &analysis.Report{}
	return rep, getJSON(c, "/api/analysis/"+id, nil, rep)
}

// getJSON は path を読んで v に入れる。
func getJSON(c *client.Client, path string, query neturl.Values, v any) error {
	body, err := c.GetQuery(path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%s の応答を読めません: %w", path, err)
	}
	return nil
}

func newAnalyzeDiffCmd(o *options) *cobra.Command {
	var top int
	cmd := &cobra.Command{
		Use:   "diff BEFORE [AFTER]",
		Short: "2つの走行の analyze を比べる（系列・リクエスト・エンドポイント構成比・ライフサイクル・内訳）。AFTER を省くと latest",
		Args:  groupArgs(2, cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			reps := [2]*analysis.Report{}
			for i, arg := range []string{args[0], groupArg(args[1:])} {
				id, err := resolve(cmd, c, arg)
				if err != nil {
					return err
				}
				if reps[i], err = fetchAnalysis(c, id); err != nil {
					return err
				}
			}
			d := analysis.Compare(reps[0], reps[1], top)
			return o.output(cmd, d, func(w io.Writer) { analysis.RenderDiffMarkdown(w, d) })
		},
	}
	cmd.Flags().IntVar(&top, "top", 10, "エンドポイント構成比の表に出す行数（0 で全部）")
	return cmd
}

func newAnalyzeTrendCmd(o *options) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "trend",
		Short: "直近の走行を1行ずつ並べる（得点・母数・利用者の進み方の推移）",
		Args:  groupArgs(0, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp := &analysis.RunsResponse{}
			if err := getJSON(o.client(), "/api/analysis", neturl.Values{"limit": {strconv.Itoa(limit)}}, resp); err != nil {
				return err
			}
			return o.output(cmd, resp, func(w io.Writer) { analysis.RenderRunsMarkdown(w, resp) })
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "遡る走行の数（1〜100）")
	return cmd
}

func newEndpointsCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "endpoints [GROUP]",
		Short: "エンドポイント別の件数・構成比・p50・p99・合計・エラー・499（全件）",
		Args:  groupArgs(1, cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, groupArg(args))
			if err != nil {
				return err
			}
			rep, err := fetchAnalysis(c, id)
			if err != nil {
				return err
			}
			out := struct {
				GroupId       string
				Warnings      []string `json:",omitempty"`
				EndpointStats []analysis.EndpointStat
			}{rep.GroupId, rep.Warnings, rep.EndpointStats}
			return o.output(cmd, out, func(w io.Writer) { analysis.RenderEndpointsMarkdown(w, rep) })
		},
	}
}

func newSessionsCmd(o *options) *cobra.Command {
	var lifecycle string
	var limit int
	cmd := &cobra.Command{
		Use:   "sessions [GROUP]",
		Short: "セッション（利用者）の一覧",
		Args:  groupArgs(1, cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, groupArg(args))
			if err != nil {
				return err
			}
			resp := &analysis.SessionsResponse{}
			if err := getJSON(c, "/api/analysis/"+id+"/sessions", lifecycleQuery(lifecycle), resp); err != nil {
				return err
			}
			// Markdown は残りの件数を書くので、切り詰めるのは JSON だけ
			shown := *resp
			if limit > 0 && len(shown.Sessions) > limit {
				shown.Sessions = shown.Sessions[:limit]
			}
			return o.output(cmd, shown, func(w io.Writer) { analysis.RenderSessionsMarkdown(w, resp, limit) })
		},
	}
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "", "そのライフサイクルに入ったセッションに絞る")
	cmd.Flags().IntVar(&limit, "limit", 50, "出すセッションの数（0 で全部）")
	return cmd
}

func newSessionCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "session GROUP SESSION_ID",
		Short: "1セッションの要求の列（SESSION_ID は sessions の ID 列、s001 など）",
		Args:  groupArgs(1, cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, args[0])
			if err != nil {
				return err
			}
			resp := &analysis.SessionDetail{}
			if err := getJSON(c, "/api/analysis/"+id+"/sessions/"+args[1], nil, resp); err != nil {
				return err
			}
			return o.output(cmd, resp, func(w io.Writer) { analysis.RenderSessionMarkdown(w, resp) })
		},
	}
}

func newTransitionsCmd(o *options) *cobra.Command {
	var lifecycle string
	var limit int
	cmd := &cobra.Command{
		Use:   "transitions [GROUP]",
		Short: "よくある遷移（ある要求の次に叩くエンドポイント）",
		Args:  groupArgs(1, cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, groupArg(args))
			if err != nil {
				return err
			}
			q := lifecycleQuery(lifecycle)
			q.Set("top", strconv.Itoa(limit))
			resp := &analysis.TransitionsResponse{}
			if err := getJSON(c, "/api/analysis/"+id+"/transitions", q, resp); err != nil {
				return err
			}
			return o.output(cmd, resp, func(w io.Writer) { analysis.RenderTransitionsMarkdown(w, resp) })
		},
	}
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "", "そのライフサイクルに入ったセッションに絞る")
	cmd.Flags().IntVar(&limit, "limit", 30, "出す遷移の数（1 以上）")
	return cmd
}

func lifecycleQuery(lifecycle string) neturl.Values {
	q := neturl.Values{}
	if lifecycle != "" {
		q.Set("lifecycle", lifecycle)
	}
	return q
}
