// Package analysis は1回の走行（収集グループ）について、種類をまたいだ集計を出す。
//
// alp / slow log / pprof は「サーバーの中で何が遅いか」を教えてくれるが、
// 「ベンチの利用者が何をしていて、得点がいつ・どこで生まれたか」は教えてくれない。
// ここでは httplog の生ログと dbquery の結果を時刻で揃え、次の3つを出す。
//
//   - 時系列: 一定幅の区間ごとのリクエスト数・エラー・応答時間と、SQL で取った系列（得点・母集団）
//   - ライフサイクル: エンティティ（利用者・ライドなど）が状態を順に進む様子。区間ごとの到達数と所要時間
//   - 内訳: SQL の結果表（配信者別の得点など）
//
// 回ごとの知識（どの SQL が得点か、どの状態を順に進むか）は analysis.yml と dbquery.yml に置き、
// このパッケージには持たない。
package analysis

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultBucket = 5
	defaultTop    = 10
)

type (
	// Config は analysis.yml。
	Config struct {
		// Bucket は時系列の区間幅（秒）。
		Bucket int `yaml:"bucket"`
		// Top は内訳・エンドポイント表に出す件数。
		Top int `yaml:"top"`

		HTTPLog HTTPLogConfig `yaml:"httplog"`

		Series     []SeriesDef    `yaml:"series"`
		Breakdowns []BreakdownDef `yaml:"breakdowns"`
		Lifecycles []LifecycleDef `yaml:"lifecycles"`
	}

	HTTPLogConfig struct {
		// Label は使う httplog の Label。空なら同じ収集の httplog を全部まとめる。
		Label string `yaml:"label"`
		// TimeKey は時刻の LTSV キー。msec（nginx の $msec）か time（$time_local）。
		// 既定は msec で、行に無ければ time を見る。
		TimeKey string `yaml:"time_key"`
		// EntityKey はライフサイクルの識別子に使う LTSV キー（セッション cookie など）。
		EntityKey string `yaml:"entity_key"`
	}

	// SeriesDef は dbquery の結果から作る時系列。
	// クエリの列: t（unix 時刻。秒/ミリ秒/マイクロ秒は桁で判定）、value（省略時は件数）、
	// series（任意。あれば値ごとに別系列 "<name>:<series>" になる）。
	SeriesDef struct {
		Name  string `yaml:"name"`
		Query string `yaml:"query"`
		// Role は系列の役割。score（得点の項）/ driver（得点を生む母数）/ penalty（得点を減らすもの）。
		// 出力に載るだけで集計は変わらない。AI や比較で「どれが得点か」を設定から引けるようにする。
		Role string `yaml:"role"`
		// Agg は区間内の集約方法。sum（既定）/ count / max / last。
		Agg string `yaml:"agg"`
		// Cumulative を true にすると区間ごとの値に加えて累積を出す（得点の積み上がりを見る）。
		Cumulative bool `yaml:"cumulative"`
	}

	// BreakdownDef は dbquery の結果表をそのまま出す。value 列があれば構成比を付ける。
	BreakdownDef struct {
		Name  string `yaml:"name"`
		Query string `yaml:"query"`
	}

	// LifecycleDef はエンティティが順に進む状態の列。
	LifecycleDef struct {
		Name string `yaml:"name"`
		// Source は httplog（識別子 = httplog.entity_key、状態 = "METHOD 正規化済みURI"）か、
		// query:<dbquery の name>（列 entity, state, t）。
		Source string `yaml:"source"`
		Steps  []Step `yaml:"steps"`
	}

	Step struct {
		Name string `yaml:"name"`
		// Match は状態に当てる正規表現。
		Match string `yaml:"match"`

		re *regexp.Regexp
	}
)

const sourceQueryPrefix = "query:"

func ParseConfig(raw []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("analysis.yml を読めません: %w", err)
	}
	if cfg.Bucket <= 0 {
		cfg.Bucket = defaultBucket
	}
	if cfg.Top <= 0 {
		cfg.Top = defaultTop
	}
	for i := range cfg.Series {
		s := &cfg.Series[i]
		if s.Name == "" || s.Query == "" {
			return nil, fmt.Errorf("series には name と query が要る: %+v", *s)
		}
		switch s.Role {
		case "", "score", "driver", "penalty":
		default:
			return nil, fmt.Errorf("series %s: role は score / driver / penalty のどれか: %q", s.Name, s.Role)
		}
		switch s.Agg {
		case "":
			s.Agg = "sum"
		case "sum", "count", "max", "last":
		default:
			return nil, fmt.Errorf("series %s: agg は sum / count / max / last のどれか: %q", s.Name, s.Agg)
		}
	}
	for _, b := range cfg.Breakdowns {
		if b.Name == "" || b.Query == "" {
			return nil, fmt.Errorf("breakdowns には name と query が要る: %+v", b)
		}
	}
	for i := range cfg.Lifecycles {
		l := &cfg.Lifecycles[i]
		if l.Name == "" || len(l.Steps) == 0 {
			return nil, fmt.Errorf("lifecycles には name と steps が要る: %+v", *l)
		}
		if l.Source != "httplog" && !strings.HasPrefix(l.Source, sourceQueryPrefix) {
			return nil, fmt.Errorf("lifecycle %s: source は httplog か query:<name>", l.Name)
		}
		if l.Source == "httplog" && cfg.HTTPLog.EntityKey == "" {
			return nil, fmt.Errorf("lifecycle %s: source が httplog なら httplog.entity_key が要る", l.Name)
		}
		for j := range l.Steps {
			st := &l.Steps[j]
			re, err := regexp.Compile(st.Match)
			if err != nil {
				return nil, fmt.Errorf("lifecycle %s の状態 %s: %w", l.Name, st.Name, err)
			}
			st.re = re
			if st.Name == "" {
				st.Name = st.Match
			}
		}
	}
	return cfg, nil
}

func (l *LifecycleDef) queryName() string {
	return strings.TrimPrefix(l.Source, sourceQueryPrefix)
}
