// Package dbquery は走行直後にアプリの DB へ SQL を流し、その結果を収集グループに紐づけて保存する。
//
// アクセスログからは「何が得点になったか」が分からないことが多い（金額・距離などはリクエストの
// 本文や DB にしかない）。走行で書き込まれた行を集計すれば得点の内訳や推移を復元できるが、
// 次の走行の initialize で消えるので、走行直後に吸い出して残しておく。
//
// どの SQL を流すかは回ごとに違うので dbquery.yml に置く。ここでは結果を解釈しない
// （列名と行をそのまま保存する）。解釈は analysis パッケージが行う。
package dbquery

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

type (
	// Config は dbquery.yml。
	Config struct {
		// DSN は go-sql-driver/mysql の形式。observer からはトンネル越しに繋ぐ。
		DSN string `yaml:"dsn"`
		// Timeout は1クエリあたりの上限。既定 30s。
		Timeout string `yaml:"timeout,omitempty"`
		Queries []Query `yaml:"queries"`
	}

	// Query は1本の SQL。SQL 中の {{since}} / {{until}} は走行区間の unix 秒に置換される。
	Query struct {
		Name string `yaml:"name"`
		SQL  string `yaml:"sql"`
	}

	// Result は snapshot の本体として保存される JSON。
	Result struct {
		Since   int64
		Until   int64
		Queries []QueryResult
	}

	QueryResult struct {
		Name    string
		Columns []string
		// Rows は各セルを文字列で持つ。数値かどうかの解釈は読む側に任せる。
		Rows  [][]*string
		Error string `json:",omitempty"`
		// Elapsed はクエリにかかった秒数。重いクエリを dbquery.yml から外す判断に使う。
		Elapsed float64
	}
)

func ParseConfig(raw []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse dbquery config: %w", err)
	}
	seen := map[string]bool{}
	for _, q := range cfg.Queries {
		if q.Name == "" || q.SQL == "" {
			return nil, fmt.Errorf("query needs both name and sql: %+v", q)
		}
		if seen[q.Name] {
			return nil, fmt.Errorf("duplicate query name: %s", q.Name)
		}
		seen[q.Name] = true
	}
	return cfg, nil
}

func (c *Config) timeout() time.Duration {
	if d, err := time.ParseDuration(c.Timeout); err == nil && d > 0 {
		return d
	}
	return 30 * time.Second
}

// Expand は {{since}} / {{until}} を置換する。
func Expand(q string, since, until int64) string {
	return strings.NewReplacer(
		"{{since}}", strconv.FormatInt(since, 10),
		"{{until}}", strconv.FormatInt(until, 10),
	).Replace(q)
}

// Run は全クエリを流す。1本が失敗しても他は続け、失敗は QueryResult.Error に残す。
// DB に繋がらないときだけ error を返す。
func Run(cfg *Config, since, until int64) (*Result, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("dsn is empty")
	}
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout())
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to connect db: %w", err)
	}

	res := &Result{Since: since, Until: until}
	for _, q := range cfg.Queries {
		res.Queries = append(res.Queries, runOne(db, cfg.timeout(), q, since, until))
	}
	return res, nil
}

func runOne(db *sql.DB, timeout time.Duration, q Query, since, until int64) QueryResult {
	r := QueryResult{Name: q.Name}
	start := time.Now()
	defer func() { r.Elapsed = time.Since(start).Seconds() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, Expand(q.SQL, since, until))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Columns = cols

	for rows.Next() {
		raw := make([]sql.RawBytes, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			r.Error = err.Error()
			return r
		}
		row := make([]*string, len(cols))
		for i, b := range raw {
			if b != nil {
				s := string(b)
				row[i] = &s
			}
		}
		r.Rows = append(r.Rows, row)
	}
	if err := rows.Err(); err != nil {
		r.Error = err.Error()
	}
	return r
}
