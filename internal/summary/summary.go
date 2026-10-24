package summary

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/smsnk/pprotein/internal/client"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/score"
)

type (
	// Group は1回の収集（GroupId）の中身をまとめたもの。
	Group struct {
		ID         string
		Datetime   time.Time
		Repository *git.RepositoryInfo
		Score      *score.Value

		HTTPLog  []LabeledEndpoints
		SlowLog  []LabeledQueries
		Profiles []LabeledFuncs
		Resource []LabeledText
		Memo     []LabeledText

		// Pending / Failed は収集がまだ終わっていない / 失敗したエントリ。
		Pending []string
		Failed  []string
	}

	LabeledEndpoints struct {
		Label     string
		Endpoints []Endpoint
	}
	LabeledQueries struct {
		Label   string
		Queries []Query
	}
	LabeledFuncs struct {
		Label string
		Funcs []Func
	}
	LabeledText struct {
		Label string
		Text  string
	}
)

// Load は GroupId（空なら最新）の中身を集めて Group にする。
// types を渡すと、その収集タイプだけを読む（diff に pprof のプロファイルは要らない）。
func Load(c *client.Client, groupID string, topN int, types ...string) (*Group, error) {
	entries, err := c.AllEntries()
	if err != nil {
		return nil, err
	}

	if groupID == "" {
		ids := client.GroupIDs(entries)
		if len(ids) == 0 {
			return nil, fmt.Errorf("収集がありません（まず pprotein-cli collect を実行してください）")
		}
		groupID = ids[0]
	}

	g := &Group{ID: groupID}
	mine := []client.Entry{}
	for _, e := range entries {
		if e.Snapshot.GroupId == groupID {
			mine = append(mine, e)
		}
	}
	if len(mine) == 0 {
		return nil, fmt.Errorf("group が見つかりません: %s", groupID)
	}
	// 同じ label の中は収集タイプの順のまま（警告の並びが実行ごとに変わらないように）
	sort.SliceStable(mine, func(i, j int) bool {
		return mine[i].Snapshot.Label < mine[j].Snapshot.Label
	})

	for _, e := range mine {
		s := e.Snapshot
		if g.Datetime.IsZero() || s.Datetime.Before(g.Datetime) {
			g.Datetime = s.Datetime
		}
		if g.Repository == nil && s.Repository != nil && s.Repository.Hash != "" {
			g.Repository = s.Repository
		}
		if len(types) > 0 && !slices.Contains(types, s.Type) {
			continue
		}

		switch e.Status {
		case "pending":
			g.Pending = append(g.Pending, fmt.Sprintf("%s:%s", s.Type, s.Label))
			continue
		case "fail":
			g.Failed = append(g.Failed, fmt.Sprintf("%s:%s (%s)", s.Type, s.Label, e.Message))
			continue
		}

		// 読めなかったものは黙って飛ばさず Failed に出す。節が丸ごと消えると「計測が無い」と読まれる。
		var err error
		switch s.Type {
		case "score":
			var v *score.Value
			if v, err = c.Score(s.ID); err == nil {
				g.Score = v
			}
		case "httplog":
			var tsv string
			if tsv, err = c.Content(s.Type, s.ID); err == nil {
				g.HTTPLog = append(g.HTTPLog, LabeledEndpoints{s.Label, head(ParseEndpoints(tsv), topN)})
			}
		case "slowlog":
			var tsv string
			if tsv, err = c.Content(s.Type, s.ID); err == nil {
				g.SlowLog = append(g.SlowLog, LabeledQueries{s.Label, head(ParseQueries(tsv), topN)})
			}
		case "resource":
			var text string
			if text, err = c.Content(s.Type, s.ID); err == nil {
				g.Resource = append(g.Resource, LabeledText{s.Label, text})
			}
		case "memo":
			var text string
			if text, err = c.Content(s.Type, s.ID); err == nil {
				g.Memo = append(g.Memo, LabeledText{s.Label, text})
			}
		case "pprof":
			var raw []byte
			if raw, err = c.RawData(s.Type, s.ID); err == nil {
				var funcs []Func
				if funcs, err = ParseProfile(raw); err == nil {
					g.Profiles = append(g.Profiles, LabeledFuncs{s.Label, head(funcs, topN)})
				}
			}
		}
		if err != nil {
			g.Failed = append(g.Failed, fmt.Sprintf("%s:%s (%v)", s.Type, s.Label, err))
		}
	}

	return g, nil
}

func head[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}
