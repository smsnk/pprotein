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
		GroupId  string
		Datetime time.Time
		Score    *score.Brief `json:",omitempty"`
		Commit   *git.Commit  `json:",omitempty"`
		// Warnings は集計に含めていないエントリ（収集中・失敗・中身を読めなかったもの）。
		Warnings []string `json:",omitempty"`

		HTTPLog  []LabeledEndpoints `json:",omitempty"`
		SlowLog  []LabeledQueries   `json:",omitempty"`
		Profiles []LabeledFuncs     `json:",omitempty"`
		Resource []LabeledText      `json:",omitempty"`
		Memo     []LabeledText      `json:",omitempty"`
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

// Types は Group に節がある収集タイプ。ptqd / dbquery は読まない（警告も出さない）。
var Types = []string{"score", "httplog", "slowlog", "resource", "pprof", "memo"}

// Load は GroupId の収集の中身を集めて Group にする。
// types を渡すと、その収集タイプだけを読む（diff に pprof のプロファイルは要らない）。既定は Types。
func Load(c *client.Client, groupID string, topN int, types ...string) (*Group, error) {
	if len(types) == 0 {
		types = Types
	}
	entries, err := c.AllEntries()
	if err != nil {
		return nil, err
	}

	g := &Group{GroupId: groupID}
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

	newestScore := client.Newest(entries, "score", groupID)
	for _, e := range mine {
		s := e.Snapshot
		if g.Datetime.IsZero() || s.Datetime.Before(g.Datetime) {
			g.Datetime = s.Datetime
		}
		if g.Commit == nil {
			g.Commit = s.Repository.Commit()
		}
		if !slices.Contains(types, s.Type) {
			continue
		}

		if w := client.Warning(e); w != "" {
			g.Warnings = append(g.Warnings, w)
			continue
		}

		// 読めなかったものも黙って飛ばさず警告に出す。節が丸ごと消えると「計測が無い」と読まれる。
		var err error
		switch s.Type {
		case "score":
			// スコアが複数あれば新しいものだけ読む（analyze と同じ）
			if s.ID != newestScore.Snapshot.ID {
				continue
			}
			var v *score.Value
			if v, err = c.Score(s.ID); err == nil {
				g.Score = v.Brief()
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
			g.Warnings = append(g.Warnings, fmt.Sprintf("%s の中身を読めなかった（%v）。この集計に含めていない", e.Name(), err))
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
