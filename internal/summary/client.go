// Package summary は pprotein の API を叩いて計測結果を取り出し、
// テキスト（Markdown）や JSON に整形する。CLI から使う。
package summary

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/score"
)

// Entry は /api/<type> が返すエントリ。
type Entry struct {
	Snapshot struct {
		Type       string
		ID         string
		Datetime   time.Time
		Repository *git.RepositoryInfo
		GroupId    string
		Label      string
		Duration   int
	}
	Status  string
	Message string
}

// Client は pprotein の HTTP API クライアント。
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) get(path string) ([]byte, error) {
	u, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, fmt.Errorf("failed to request %s: %w", u, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %s: status=%d body=%s", u, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// Entries は指定した収集タイプのエントリ一覧を返す。
func (c *Client) Entries(typ string) ([]Entry, error) {
	body, err := c.get("/api/" + typ)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s entries: %w", typ, err)
	}
	return entries, nil
}

// Content は処理済みの結果（alp の TSV、resource の要約テキストなど）を返す。
func (c *Client) Content(typ, id string) (string, error) {
	body, err := c.get(fmt.Sprintf("/api/%s/%s", typ, id))
	return string(body), err
}

// RawData は生のスナップショット（pprof のプロファイルなど）を返す。
func (c *Client) RawData(typ, id string) ([]byte, error) {
	return c.get(fmt.Sprintf("/api/%s/data/%s", typ, id))
}

// Score は収集に紐づいたスコアを返す。無ければ nil。
func (c *Client) Score(id string) (*score.Value, error) {
	body, err := c.get("/api/score/" + id)
	if err != nil {
		return nil, err
	}
	v := &score.Value{}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal score: %w", err)
	}
	return v, nil
}

// StartCollect は収集を開始する。
func (c *Client) StartCollect() error {
	_, err := c.get("/api/group/collect")
	return err
}

// CollectTypes は summary が読む収集タイプ。表示する順に並べてある。
var CollectTypes = []string{"score", "httplog", "slowlog", "resource", "pprof", "memo"}

// AllEntries は全タイプのエントリをまとめて取る。
// 収集タイプが存在しない（古い pprotein）場合はそのタイプを黙って飛ばす。
func (c *Client) AllEntries() ([]Entry, error) {
	var all []Entry
	var firstErr error
	for _, typ := range CollectTypes {
		entries, err := c.Entries(typ)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, entries...)
	}
	if all == nil && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

// GroupIDs は新しい順の GroupId 一覧を返す。
func GroupIDs(entries []Entry) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, e := range entries {
		if e.Snapshot.GroupId == "" || seen[e.Snapshot.GroupId] {
			continue
		}
		seen[e.Snapshot.GroupId] = true
		ids = append(ids, e.Snapshot.GroupId)
	}
	// GroupId は "YYYY-mm-dd_HH-MM-SS.ffffff" なので文字列比較で時系列に並ぶ
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
