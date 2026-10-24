package summary

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/pprof/profile"
)

type (
	// Endpoint は alp の1行。列は alp の設定で変わるので、ヘッダ名で引く。
	Endpoint struct {
		Method string
		Uri    string
		Count  float64
		Sum    float64 `json:"SumSec"`
		Avg    float64 `json:"AvgSec"`
		P99    float64 `json:"P99Sec"`
		Max    float64 `json:"MaxSec"`
	}

	// Query は slp の1行。
	Query struct {
		Query string
		Count float64
		Sum   float64 `json:"SumSec"`
		Avg   float64 `json:"AvgSec"`
		Max   float64 `json:"MaxSec"`
		// RowsExamined は1回あたりの走査行数の平均。インデックスの当たり外れが出る。
		RowsExamined float64 `json:"RowsExaminedAvg"`
	}

	// Func は pprof の1関数。Flat / Cum の単位は Unit（時間のプロファイルなら seconds）。
	Func struct {
		Name string
		Flat float64
		Cum  float64
		Unit string
		// FlatShare は全サンプルの flat に占める割合（0〜1）。
		FlatShare float64
	}
)

func (e Endpoint) Key() string { return e.Method + " " + e.Uri }
func (q Query) Key() string    { return q.Query }

// parseTSV はヘッダ付き TSV を「列名 -> 値」のマップの列にする。
func parseTSV(tsv string) []map[string]string {
	lines := strings.Split(strings.ReplaceAll(tsv, "\r\n", "\n"), "\n")
	var header []string
	rows := []map[string]string{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if header == nil {
			header = fields
			continue
		}
		row := make(map[string]string, len(header))
		for i, name := range header {
			if i < len(fields) {
				row[strings.ToUpper(strings.TrimSpace(name))] = fields[i]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// pick は候補の列名を順に試して、最初に見つかった値を float で返す。
func pick(row map[string]string, names ...string) float64 {
	for _, n := range names {
		if v, ok := row[n]; ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return f
			}
		}
	}
	return 0
}

func pickStr(row map[string]string, names ...string) string {
	for _, n := range names {
		if v, ok := row[n]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ParseEndpoints は alp の TSV を読む。SUM の大きい順に並べる。
func ParseEndpoints(tsv string) []Endpoint {
	out := []Endpoint{}
	for _, row := range parseTSV(tsv) {
		e := Endpoint{
			Method: pickStr(row, "METHOD"),
			Uri:    pickStr(row, "URI", "URL"),
			Count:  pick(row, "COUNT"),
			Sum:    pick(row, "SUM"),
			Avg:    pick(row, "AVG"),
			P99:    pick(row, "P99"),
			Max:    pick(row, "MAX"),
		}
		if e.Uri == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sum > out[j].Sum })
	return out
}

// ParseQueries は slp の TSV を読む。合計クエリ時間の大きい順に並べる。
func ParseQueries(tsv string) []Query {
	out := []Query{}
	for _, row := range parseTSV(tsv) {
		// 列名は slp v0.2.1 の実出力に合わせてある（Sum(QueryTime) など）。
		q := Query{
			Query:        pickStr(row, "QUERY"),
			Count:        pick(row, "COUNT"),
			Sum:          pick(row, "SUM(QUERYTIME)", "SUM(QUERY TIME)", "SUM"),
			Avg:          pick(row, "AVG(QUERYTIME)", "AVG(QUERY TIME)", "AVG"),
			Max:          pick(row, "MAX(QUERYTIME)", "MAX(QUERY TIME)", "MAX"),
			RowsExamined: pick(row, "AVG(ROWSEXAMINED)", "AVG(ROWS EXAMINED)"),
		}
		if q.Query == "" {
			continue
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sum > out[j].Sum })
	return out
}

// ParseProfile は pprof のプロファイルから関数ごとの flat / cum を出す。
func ParseProfile(raw []byte) ([]Func, error) {
	p, err := profile.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("プロファイルを読めません: %w", err)
	}
	if len(p.SampleType) == 0 {
		return nil, fmt.Errorf("プロファイルにサンプルの種類がありません")
	}

	// CPU プロファイルなら最後が nanoseconds。fgprof も同じ形。時間は秒に直す。
	idx := len(p.SampleType) - 1
	unit, div := p.SampleType[idx].Unit, 1.0
	if unit == "nanoseconds" {
		unit, div = "seconds", 1e9
	}

	flat := map[string]float64{}
	cum := map[string]float64{}
	totalFlat := 0.0

	for _, s := range p.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := float64(s.Value[idx])
		if v == 0 {
			continue
		}

		// Location[0] が葉。inline された関数は1つの Location の Line に並び、Line[0] が最も内側。
		// flat は葉の Line[0] にだけ付ける。cum は1サンプル内の同じ関数を二重に数えない。
		seen := map[string]bool{}
		for i, loc := range s.Location {
			for j, line := range loc.Line {
				if line.Function == nil {
					continue
				}
				name := line.Function.Name
				if i == 0 && j == 0 {
					flat[name] += v
					totalFlat += v
				}
				if !seen[name] {
					cum[name] += v
					seen[name] = true
				}
			}
		}
	}

	out := make([]Func, 0, len(cum))
	for name, c := range cum {
		f := Func{Name: name, Flat: flat[name] / div, Cum: c / div, Unit: unit}
		if totalFlat > 0 {
			f.FlatShare = math.Round(flat[name]/totalFlat*1e4) / 1e4 // analysis と同じく 0.01% の単位に丸める
		}
		out = append(out, f)
	}
	// 同じ値なら名前の順にして、実行ごとに並びが変わらないようにする（サンプリングなので同じ値はよくある）
	sort.Slice(out, func(i, j int) bool {
		if out[i].Flat != out[j].Flat {
			return out[i].Flat > out[j].Flat
		}
		if out[i].Cum != out[j].Cum {
			return out[i].Cum > out[j].Cum
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
