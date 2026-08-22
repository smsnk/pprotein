package summary

import (
	"fmt"
	"sort"
	"time"

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
func Load(c *Client, groupID string, topN int) (*Group, error) {
	entries, err := c.AllEntries()
	if err != nil {
		return nil, err
	}

	if groupID == "" {
		ids := GroupIDs(entries)
		if len(ids) == 0 {
			return nil, fmt.Errorf("収集がありません（まず pprotein-cli collect を実行してください）")
		}
		groupID = ids[0]
	}

	g := &Group{ID: groupID}
	mine := []Entry{}
	for _, e := range entries {
		if e.Snapshot.GroupId == groupID {
			mine = append(mine, e)
		}
	}
	if len(mine) == 0 {
		return nil, fmt.Errorf("group が見つかりません: %s", groupID)
	}
	sort.Slice(mine, func(i, j int) bool {
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

		switch e.Status {
		case "pending":
			g.Pending = append(g.Pending, fmt.Sprintf("%s:%s", s.Type, s.Label))
			continue
		case "fail":
			g.Failed = append(g.Failed, fmt.Sprintf("%s:%s (%s)", s.Type, s.Label, e.Message))
			continue
		}

		switch s.Type {
		case "score":
			if v, err := c.Score(s.ID); err == nil {
				g.Score = v
			}
		case "httplog":
			if tsv, err := c.Content(s.Type, s.ID); err == nil {
				g.HTTPLog = append(g.HTTPLog, LabeledEndpoints{s.Label, head(ParseEndpoints(tsv), topN)})
			}
		case "slowlog":
			if tsv, err := c.Content(s.Type, s.ID); err == nil {
				g.SlowLog = append(g.SlowLog, LabeledQueries{s.Label, head(ParseQueries(tsv), topN)})
			}
		case "resource":
			if text, err := c.Content(s.Type, s.ID); err == nil {
				g.Resource = append(g.Resource, LabeledText{s.Label, text})
			}
		case "memo":
			if text, err := c.Content(s.Type, s.ID); err == nil {
				g.Memo = append(g.Memo, LabeledText{s.Label, text})
			}
		case "pprof":
			raw, err := c.RawData(s.Type, s.ID)
			if err != nil {
				continue
			}
			funcs, err := ParseProfile(raw)
			if err != nil {
				g.Failed = append(g.Failed, fmt.Sprintf("pprof:%s (%v)", s.Label, err))
				continue
			}
			g.Profiles = append(g.Profiles, LabeledFuncs{s.Label, head(funcs, topN)})
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
