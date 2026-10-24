package main

import (
	"fmt"
	"io"

	"github.com/smsnk/pprotein/internal/client"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/md"
	"github.com/smsnk/pprotein/internal/score"
	"github.com/smsnk/pprotein/internal/summary"
	"github.com/spf13/cobra"
)

func newSummaryCmd(o *options) *cobra.Command {
	var top int
	var wait bool
	cmd := &cobra.Command{
		Use:   "summary [GROUP]",
		Short: "1回の収集を要約する（httplog / slowlog / pprof / resource / memo）",
		Args:  groupArgs(1, cobra.MaximumNArgs(1)),
		// 打ち間違えたサブコマンドを案内する（groupArgs）
		SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			id, err := resolve(cmd, c, groupArg(args))
			if err != nil {
				return err
			}
			if wait {
				if _, err := waitGroup(cmd, c, o.timeout, func([]client.Entry) string { return id }, summary.Types...); err != nil {
					return err
				}
			}
			return o.printSummary(cmd, c, id, top)
		},
	}
	cmd.Flags().IntVar(&top, "top", 10, "各表に出す行数（0 で全部）")
	cmd.Flags().BoolVar(&wait, "wait", false, "その収集のうち summary が読むエントリが確定するまで待ってから出す（上限は --timeout）")
	return cmd
}

func (o *options) printSummary(cmd *cobra.Command, c *client.Client, id string, top int) error {
	g, err := summary.Load(c, id, top)
	if err != nil {
		return err
	}
	return o.output(cmd, g, func(w io.Writer) { fmt.Fprint(w, g.Markdown()) })
}

func newSummaryDiffCmd(o *options) *cobra.Command {
	var top int
	cmd := &cobra.Command{
		Use:   "diff BEFORE [AFTER]",
		Short: "2つの収集の httplog / slowlog を、増減（IMPACT）の大きい順に比べる。AFTER を省くと latest",
		Args:  groupArgs(2, cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			groups := [2]*summary.Group{}
			for i, arg := range []string{args[0], groupArg(args[1:])} {
				id, err := resolve(cmd, c, arg)
				if err != nil {
					return err
				}
				// 比べるのは httplog / slowlog だけなので、pprof のプロファイルなどは読まない
				if groups[i], err = summary.Load(c, id, 0, "score", "httplog", "slowlog"); err != nil {
					return err
				}
			}
			d := summary.Compare(groups[0], groups[1], top)
			return o.output(cmd, d, func(w io.Writer) { fmt.Fprint(w, d.Markdown()) })
		},
	}
	cmd.Flags().IntVar(&top, "top", 10, "各表に出す行数（0 で全部）")
	return cmd
}

func newGroupsCmd(o *options) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "収集の一覧（新しい順。コミット・スコア付き）",
		Args:  groupArgs(0, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			entries, err := c.AllEntries()
			if err != nil {
				return err
			}
			ids := client.GroupIDs(entries)
			total := len(ids)
			if limit > 0 && len(ids) > limit {
				ids = ids[:limit]
			}
			rows := groupRows(c, entries, ids)
			return o.output(cmd, rows, func(w io.Writer) {
				fmt.Fprintf(w, "# groups（新しい順に %d 件 / 全 %d 件）\n\n", len(rows), total)
				md.Row(w, []string{"GroupId", "commit", "score", "entries"})
				md.Sep(w, 4)
				for _, r := range rows {
					commit, scoreCell := "", "-"
					if r.Commit != nil {
						commit = r.Commit.ShortHash()
					}
					if r.Score != nil {
						scoreCell = r.Score.String()
					}
					md.Row(w, []string{r.GroupId, commit, scoreCell, md.Int(r.Entries)})
				}
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "出す収集の数（0 で全部）")
	return cmd
}

type groupRow struct {
	GroupId string
	Commit  *git.Commit  `json:",omitempty"`
	Score   *score.Brief `json:",omitempty"`
	Entries int
}

func groupRows(c *client.Client, entries []client.Entry, ids []string) []groupRow {
	rows := []groupRow{}
	for _, id := range ids {
		r := groupRow{GroupId: id}
		for _, e := range entries {
			if e.Snapshot.GroupId != id {
				continue
			}
			r.Entries++
			if r.Commit == nil {
				r.Commit = e.Snapshot.Repository.Commit()
			}
		}
		// スコアが複数あれば新しいものを使う（analyze と同じ）。一覧なので、読めなくても止めずに - と出す
		if e := client.Newest(entries, "score", id); e != nil {
			if v, err := c.Score(e.Snapshot.ID); err == nil {
				r.Score = v.Brief()
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func newCollectCmd(o *options) *cobra.Command {
	var top int
	var noWait bool
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "収集を開始し、完了まで待って summary を出す",
		Args:  groupArgs(0, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := o.client()
			before, err := c.AllEntries()
			if err != nil {
				return err
			}
			known := map[string]bool{}
			for _, id := range client.GroupIDs(before) {
				known[id] = true
			}

			if err := c.StartCollect(); err != nil {
				return fmt.Errorf("収集の開始に失敗しました: %w", err)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "収集を開始しました")
			if noWait {
				return nil
			}

			id, err := waitGroup(cmd, c, o.timeout, func(entries []client.Entry) string {
				for _, id := range client.GroupIDs(entries) {
					if !known[id] {
						return id
					}
				}
				return ""
			})
			if err != nil {
				return err
			}
			return o.printSummary(cmd, c, id, top)
		},
	}
	cmd.Flags().IntVar(&top, "top", 10, "各表に出す行数（0 で全部）")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "収集を開始するだけで、完了を待たない")
	return cmd
}
