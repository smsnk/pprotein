package analysis

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const timeLocalLayout = "02/Jan/2006:15:04:05 -0700"

// Request は LTSV 1行ぶん。
type Request struct {
	// T は unix 秒（小数あり）。
	T        float64
	Method   string
	Endpoint string // matching_groups でまとめた URI
	// URI はまとめる前の URI（クエリ文字列を含む）、Host は vhost（配信者のサブドメインなど）。
	// セッションの導線を1件ずつ見るときに使う。
	URI     string
	Host    string
	Status  int
	ReqTime float64
	Entity  string
}

// Key はライフサイクルの状態に使う文字列。
func (r *Request) Key() string {
	return r.Method + " " + r.Endpoint
}

// Normalizer は alp.yml の matching_groups で URI をまとめる。
// alp と同じく、クエリ文字列を落としてから先頭から順に当て、最初に当たった正規表現を返す。
type Normalizer struct {
	groups []*regexp.Regexp
}

func NewNormalizer(alpConfig []byte) (*Normalizer, error) {
	cfg := struct {
		MatchingGroups []string `yaml:"matching_groups"`
	}{}
	if len(alpConfig) > 0 {
		if err := yaml.Unmarshal(alpConfig, &cfg); err != nil {
			return nil, fmt.Errorf("alp の設定を読めません: %w", err)
		}
	}
	n := &Normalizer{}
	for _, g := range cfg.MatchingGroups {
		re, err := regexp.Compile(g)
		if err != nil {
			return nil, fmt.Errorf("matching_groups の %q が正規表現として正しくない: %w", g, err)
		}
		n.groups = append(n.groups, re)
	}
	return n, nil
}

func (n *Normalizer) Normalize(uri string) string {
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri = uri[:i]
	}
	for _, re := range n.groups {
		if re.MatchString(uri) {
			return re.String()
		}
	}
	return uri
}

// ParseLTSV は nginx の LTSV ログを読む。時刻が読めない行は飛ばし、その件数を返す。
func ParseLTSV(data []byte, n *Normalizer, timeKey, entityKey string) ([]Request, int) {
	if timeKey == "" {
		timeKey = "msec"
	}
	var (
		reqs    []Request
		skipped int
	)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		f := splitLTSV(sc.Text())
		t, ok := parseTime(f, timeKey)
		if !ok {
			skipped++
			continue
		}
		r := Request{
			T:        t,
			Method:   f["method"],
			Endpoint: n.Normalize(f["uri"]),
			URI:      f["uri"],
			Host:     cleanValue(f["vhost"]),
			Entity:   cleanValue(f[entityKey]),
		}
		r.Status, _ = strconv.Atoi(f["status"])
		r.ReqTime, _ = strconv.ParseFloat(f["reqtime"], 64)
		reqs = append(reqs, r)
	}
	return reqs, skipped
}

func splitLTSV(line string) map[string]string {
	f := map[string]string{}
	for _, kv := range strings.Split(line, "\t") {
		if i := strings.IndexByte(kv, ':'); i > 0 {
			f[kv[:i]] = kv[i+1:]
		}
	}
	return f
}

func parseTime(f map[string]string, key string) (float64, bool) {
	if v, ok := f[key]; ok && v != "" && v != "-" {
		if key == "time" {
			if t, err := time.Parse(timeLocalLayout, v); err == nil {
				return float64(t.Unix()), true
			}
		} else if x, err := strconv.ParseFloat(v, 64); err == nil {
			return x, true
		}
	}
	if key != "time" {
		return parseTime(f, "time")
	}
	return 0, false
}

// cleanValue は nginx が値なしのときに出す "-" を空にする。
func cleanValue(v string) string {
	if v == "-" {
		return ""
	}
	return v
}

// NormalizeUnixTime は秒・ミリ秒・マイクロ秒の unix 時刻を秒に揃える。
// SQL 側で単位を揃えなくてよいように、桁で判定する。
func NormalizeUnixTime(x float64) float64 {
	switch {
	case x > 1e17:
		return x / 1e9
	case x > 1e14:
		return x / 1e6
	case x > 1e11:
		return x / 1e3
	}
	return x
}
