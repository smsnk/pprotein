// Package client は pprotein の HTTP API を読む。
// CLI と、自分自身の API から材料を読むサーバー側の集計（analysis / trend）が使う。
package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/smsnk/pprotein/internal/dbquery"
	"github.com/smsnk/pprotein/internal/git"
	"github.com/smsnk/pprotein/internal/md"
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

func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) get(path string) ([]byte, error) {
	return c.GetQuery(path, nil)
}

// GetQuery は path にクエリ文字列を付けて GET する（path に ? を含めると JoinPath でエスケープされるため）。
func (c *Client) GetQuery(path string, query url.Values) ([]byte, error) {
	u, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return nil, fmt.Errorf("URL が正しくありません: %w", err)
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", u, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s の応答を読めません: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s が %d を返しました: %s", u, resp.StatusCode, errorMessage(body))
	}
	return body, nil
}

// errorMessage はエラー応答の本文から、人が読むメッセージを取り出す。echo のエラーは {"message": ...} の JSON。
func errorMessage(body []byte) string {
	var e struct{ Message string }
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return md.Clip(strings.TrimSpace(string(body)), 200)
}

// Entries は指定した収集タイプのエントリ一覧を返す。
func (c *Client) Entries(typ string) ([]Entry, error) {
	body, err := c.get("/api/" + typ)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("%s の一覧を読めません: %w", typ, err)
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

// Config は収集タイプの設定ファイル（alp.yml など）を返す。
func (c *Client) Config(typ string) ([]byte, error) {
	return c.get(fmt.Sprintf("/api/%s/config", typ))
}

// Score は収集に紐づいたスコアを返す。無ければ nil。
func (c *Client) Score(id string) (*score.Value, error) {
	body, err := c.get("/api/score/" + id)
	if err != nil {
		return nil, err
	}
	v := &score.Value{}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, fmt.Errorf("スコアを読めません: %w", err)
	}
	return v, nil
}

// StartCollect は収集を開始する。
func (c *Client) StartCollect() error {
	_, err := c.get("/api/group/collect")
	return err
}

// Types は pprotein の収集タイプ。
var Types = []string{"score", "httplog", "slowlog", "ptqd", "resource", "pprof", "memo", "dbquery"}

// AllEntries は全タイプのエントリをまとめて取る。
// 収集タイプが存在しない（古い pprotein）場合はそのタイプを黙って飛ばす。
func (c *Client) AllEntries() ([]Entry, error) {
	var all []Entry
	var firstErr error
	for _, typ := range Types {
		entries, err := c.Entries(typ)
		if err != nil {
			// 繋がらない・応答が無いなら残りのタイプも同じ。タイムアウトをタイプの数だけ待たない
			if errors.As(err, new(*url.Error)) {
				return nil, err
			}
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

// Latest は最新の収集の GroupId を返す。収集中・失敗したものも含める。
// 1つ前の走行を指すと、走行直後の収集中や新しい走行の失敗が前の走行の結果で隠れ、取り違えに気づけない。
// GroupId の形でないもの（API に手で送ったスコアやメモ）は時刻の順に並ばないので除く。
func Latest(entries []Entry) (string, error) {
	for _, id := range GroupIDs(entries) {
		if _, err := time.Parse(dbquery.GroupIdLayout, id); err == nil {
			return id, nil
		}
	}
	return "", errors.New("収集がありません")
}

// Newest は収集 groupId の typ のエントリのうち、確定した（ok の）いちばん新しいものを返す。無ければ nil。
// ベンチを回し直してスコアや dbquery を送り直すと、同じ収集に複数できる。
func Newest(entries []Entry, typ, groupId string) *Entry {
	var newest *Entry
	for i, e := range entries {
		if e.Snapshot.Type != typ || e.Snapshot.GroupId != groupId || e.Status != "ok" {
			continue
		}
		if newest == nil || e.Snapshot.Datetime.After(newest.Snapshot.Datetime) {
			newest = &entries[i]
		}
	}
	return newest
}

// Warning は確定していない（収集中・失敗の）エントリを警告文にする。確定していれば空。
// 黙って飛ばすと、収集中の走行がリクエスト 0 件として読まれてしまう。
func Warning(e Entry) string {
	switch e.Status {
	case "pending":
		return e.Name() + " は収集中（pending）。この集計に含めていない。完了してから再実行する"
	case "fail":
		return fmt.Sprintf("%s は収集に失敗した（%s）。この集計に含めていない", e.Name(), e.Message)
	}
	return ""
}

// Name は警告などに出すエントリの名前（「httplog isu1」）。
func (e Entry) Name() string {
	if e.Snapshot.Label == "" {
		return e.Snapshot.Type
	}
	return e.Snapshot.Type + " " + e.Snapshot.Label
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
