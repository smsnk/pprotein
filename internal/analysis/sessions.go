package analysis

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/smsnk/pprotein/internal/md"
)

// セッション（httplog の entity_key で束ねた利用者）単位で導線を見る。
//
// ライフサイクルの集計は「何人がどこまで進んだか」しか出さない。止まった利用者が直前に何をしていたか、
// ベンチのシナリオがどんな順でエンドポイントを叩くかは、1人ずつの列と遷移を見ないと分からない。
// ベンチのソースが無い本番でも、ここからシナリオの形を推測できる。
//
// 対象は source: httplog のライフサイクルだけ（query: のライフサイクルはエンティティがセッションではない）。

const (
	transitionStart = "(開始)"
	transitionEnd   = "(終了)"
)

type (
	SessionsResponse struct {
		GroupId string
		// Lifecycles は source: httplog のライフサイクルと状態名（Sessions[].Lifecycles の並び）。
		Lifecycles []LifecycleSteps
		// Total はセッションの総数（lifecycle で絞る前）。
		Total    int
		Sessions []SessionSummary
	}

	LifecycleSteps struct {
		Name  string
		Steps []string
	}

	SessionSummary struct {
		// ID は開始時刻順の連番（s001, s002, ...）。同じ材料なら同じ番号になる。
		ID string
		// Hosts は訪れた vhost（多い順、最大3件）。ISUCON13 なら配信者のサブドメイン。
		Hosts      []string `json:",omitempty"`
		StartSec   float64
		EndSec     float64
		Requests   int
		Errors     int // 4xx + 5xx（499 を除く）
		S499       int
		MaxReqSec  float64
		Lifecycles []SessionReach
	}

	SessionReach struct {
		Name string
		// Reached は順に到達した状態の数（0 ならこのライフサイクルに入っていない）。
		Reached int
		// LastStep は最後に到達した状態の名前。
		LastStep string `json:",omitempty"`
	}

	SessionDetail struct {
		GroupId  string
		Summary  SessionSummary
		Requests []SessionRequest
	}

	SessionRequest struct {
		OffsetSec  float64
		Method     string
		Endpoint   string
		URI        string
		Host       string `json:",omitempty"`
		Status     int
		ReqTimeSec float64
		// Steps はこの要求で進んだライフサイクルの状態（"viewer:enter" の形）。
		Steps []string `json:",omitempty"`
	}

	TransitionsResponse struct {
		GroupId   string
		Lifecycle string `json:",omitempty"`
		// Sessions は集計したセッションの数。
		Sessions    int
		Transitions []Transition
	}

	// Transition は「From の次に To を叩いた」回数。From が (開始) なら最初の要求、To が (終了) なら最後の要求。
	Transition struct {
		From  string
		To    string
		Count int
		// ShareOfFrom は From の次の要求のうち、To が占める割合（0〜1）。
		ShareOfFrom float64
		// GapP50Sec は From から To までの間隔の中央値。
		GapP50Sec float64
	}

	// sessionIndex は1走行のセッションを時刻順の要求列にまとめたもの。
	sessionIndex struct {
		groupId    string
		origin     float64
		lifecycles []LifecycleDef
		order      []string // ID（開始時刻順）
		sessions   map[string]*sessionData
	}

	sessionData struct {
		entity  string
		summary SessionSummary
		reqs    []Request
		// advanced[i] は reqs[i] で進んだ状態
		advanced [][]string
	}
)

func ms(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func buildSessionIndex(cfg *Config, in *Input) *sessionIndex {
	idx := &sessionIndex{groupId: in.GroupId, origin: in.Origin, sessions: map[string]*sessionData{}}
	for _, l := range cfg.Lifecycles {
		if l.Source == "httplog" {
			idx.lifecycles = append(idx.lifecycles, l)
		}
	}

	all := []*sessionData{}
	byEntity := map[string][]Request{}
	for _, r := range in.Requests {
		if r.Entity != "" {
			byEntity[r.Entity] = append(byEntity[r.Entity], r)
		}
	}
	for entity, reqs := range byEntity {
		sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].T < reqs[j].T })
		d := &sessionData{entity: entity, reqs: reqs, advanced: make([][]string, len(reqs))}
		s := &d.summary
		s.StartSec = ms(reqs[0].T - in.Origin)
		s.EndSec = ms(reqs[len(reqs)-1].T - in.Origin)
		s.Requests = len(reqs)
		hosts := map[string]int{}
		for _, r := range reqs {
			if r.Host != "" {
				hosts[r.Host]++
			}
			switch {
			case r.Status == 499:
				s.S499++
			case r.Status >= 400:
				s.Errors++
			}
			s.MaxReqSec = math.Max(s.MaxReqSec, ms(r.ReqTime))
		}
		s.Hosts = topKeys(hosts, 3)

		evs := make([]event, len(reqs))
		for i, r := range reqs {
			evs[i] = event{entity, r.Key(), r.T}
		}
		for _, l := range idx.lifecycles {
			cursor, _, _ := walkSteps(l.Steps, evs)
			reach := SessionReach{Name: l.Name, Reached: cursor}
			if cursor > 0 {
				reach.LastStep = l.Steps[cursor-1].Name
			}
			s.Lifecycles = append(s.Lifecycles, reach)

			// どの要求で状態が進んだか（walkSteps と同じ判定を1件ずつたどる）
			c := 0
			for i, ev := range evs {
				if c < len(l.Steps) && l.Steps[c].re.MatchString(ev.state) {
					d.advanced[i] = append(d.advanced[i], l.Name+":"+l.Steps[c].Name)
					c++
				}
			}
		}
		all = append(all, d)
	}
	// 開始時刻順に並べて連番を振る（同時刻なら要求数の多い順、最後は識別子で決めて番号を毎回同じにする）
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.summary.StartSec != b.summary.StartSec {
			return a.summary.StartSec < b.summary.StartSec
		}
		if a.summary.Requests != b.summary.Requests {
			return a.summary.Requests > b.summary.Requests
		}
		return a.entity < b.entity
	})
	for i, d := range all {
		d.summary.ID = fmt.Sprintf("s%03d", i+1)
		idx.sessions[d.summary.ID] = d
		idx.order = append(idx.order, d.summary.ID)
	}
	return idx
}

// inLifecycle は lifecycle で絞るときに、そのライフサイクルの最初の状態に入ったセッションかを返す。
func (d *sessionData) inLifecycle(name string) bool {
	if name == "" {
		return true
	}
	for _, r := range d.summary.Lifecycles {
		if r.Name == name {
			return r.Reached > 0
		}
	}
	return false
}

func (idx *sessionIndex) list(lifecycle string) *SessionsResponse {
	resp := &SessionsResponse{GroupId: idx.groupId, Total: len(idx.order), Sessions: []SessionSummary{}}
	for _, l := range idx.lifecycles {
		ls := LifecycleSteps{Name: l.Name}
		for _, st := range l.Steps {
			ls.Steps = append(ls.Steps, st.Name)
		}
		resp.Lifecycles = append(resp.Lifecycles, ls)
	}
	for _, id := range idx.order {
		d := idx.sessions[id]
		if d.inLifecycle(lifecycle) {
			resp.Sessions = append(resp.Sessions, d.summary)
		}
	}
	return resp
}

func (idx *sessionIndex) detail(id string) (*SessionDetail, bool) {
	d, ok := idx.sessions[id]
	if !ok {
		return nil, false
	}
	out := &SessionDetail{GroupId: idx.groupId, Summary: d.summary}
	for i, r := range d.reqs {
		out.Requests = append(out.Requests, SessionRequest{
			OffsetSec: ms(r.T - idx.origin), Method: r.Method, Endpoint: r.Endpoint, URI: r.URI, Host: r.Host,
			Status: r.Status, ReqTimeSec: ms(r.ReqTime), Steps: d.advanced[i],
		})
	}
	return out, true
}

// transitions は連続する2つの要求（"METHOD エンドポイント"）の組を数える。
func (idx *sessionIndex) transitions(lifecycle string, top int) *TransitionsResponse {
	resp := &TransitionsResponse{GroupId: idx.groupId, Lifecycle: lifecycle, Transitions: []Transition{}}
	type key struct{ from, to string }
	counts := map[key]int{}
	gaps := map[key][]float64{}
	fromTotal := map[string]int{}
	add := func(from, to string, gap float64) {
		k := key{from, to}
		counts[k]++
		fromTotal[from]++
		if gap >= 0 {
			gaps[k] = append(gaps[k], gap)
		}
	}
	for _, id := range idx.order {
		d := idx.sessions[id]
		if !d.inLifecycle(lifecycle) {
			continue
		}
		resp.Sessions++
		prev, prevT := transitionStart, -1.0
		for _, r := range d.reqs {
			gap := -1.0
			if prevT >= 0 {
				gap = r.T - prevT
			}
			add(prev, r.Key(), gap)
			prev, prevT = r.Key(), r.T
		}
		add(prev, transitionEnd, -1)
	}
	for k, n := range counts {
		t := Transition{From: k.from, To: k.to, Count: n}
		if fromTotal[k.from] > 0 {
			t.ShareOfFrom = math.Round(float64(n)/float64(fromTotal[k.from])*1e4) / 1e4
		}
		if g := gaps[k]; len(g) > 0 {
			sort.Float64s(g)
			t.GapP50Sec = ms(quantile(g, 0.5))
		}
		resp.Transitions = append(resp.Transitions, t)
	}
	sort.Slice(resp.Transitions, func(i, j int) bool {
		a, b := resp.Transitions[i], resp.Transitions[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	if top > 0 && len(resp.Transitions) > top {
		resp.Transitions = resp.Transitions[:top]
	}
	return resp
}

// --- handlers ---

// resolveRun は :id（latest 可）の走行を解決し、設定と、材料と設定が同じなら同じになるキャッシュのキーを返す。
// 材料が確定していない（収集中の）走行はキーが空になり、キャッシュしない。
func (h *Handler) resolveRun(c echo.Context) (*Config, string, string, error) {
	cfg, raw, err := h.loadConfig()
	if err != nil {
		return nil, "", "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	id := c.Param("id")
	if id == "latest" {
		if id, err = LatestGroup(h.client); err != nil {
			return nil, "", "", echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
	}
	entries, err := loadEntryIndex(h.client)
	if err != nil {
		return nil, "", "", echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return cfg, id, entries.fingerprint(id, hashConfig(raw)), nil
}

// sessionIndexFor は1走行の sessionIndex を返す。走行の材料と設定が変わらなければ使い回す。
func (h *Handler) sessionIndexFor(c echo.Context) (*sessionIndex, error) {
	cfg, id, key, err := h.resolveRun(c)
	if err != nil {
		return nil, err
	}
	if cfg.HTTPLog.EntityKey == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "analysis.yml の httplog.entity_key が空なのでセッションに束ねられない")
	}
	if key != "" {
		if idx, ok := h.sessions.get(key); ok {
			return idx, nil
		}
	}
	in, err := Load(h.client, cfg, id)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	idx := buildSessionIndex(cfg, in)
	if key != "" && len(in.Warnings) == 0 {
		h.sessions.put(key, idx)
	}
	return idx, nil
}

// getSessions は GET /api/analysis/:id/sessions[?lifecycle=<name>]。
func (h *Handler) getSessions(c echo.Context) error {
	idx, err := h.sessionIndexFor(c)
	if err != nil {
		return err
	}
	return compactJSON(c, idx.list(c.QueryParam("lifecycle")))
}

// getSession は GET /api/analysis/:id/sessions/:sid。
func (h *Handler) getSession(c echo.Context) error {
	idx, err := h.sessionIndexFor(c)
	if err != nil {
		return err
	}
	d, ok := idx.detail(c.Param("sid"))
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "no such session")
	}
	return compactJSON(c, d)
}

// getTransitions は GET /api/analysis/:id/transitions[?lifecycle=<name>&top=N]。
func (h *Handler) getTransitions(c echo.Context) error {
	idx, err := h.sessionIndexFor(c)
	if err != nil {
		return err
	}
	top := 30
	if v, err := strconv.Atoi(c.QueryParam("top")); err == nil && v > 0 {
		top = v
	}
	return compactJSON(c, idx.transitions(c.QueryParam("lifecycle"), top))
}

// --- Markdown（CLI） ---

func RenderSessionsMarkdown(w io.Writer, s *SessionsResponse, top int) {
	fmt.Fprintf(w, "# analyze sessions %s（%d / 全 %d セッション）\n\n", s.GroupId, len(s.Sessions), s.Total)
	head := []string{"ID", "開始", "終了", "req", "エラー", "499", "最大応答", "host"}
	for _, l := range s.Lifecycles {
		head = append(head, l.Name)
	}
	md.Row(w, head)
	md.Sep(w, len(head))
	for i, x := range s.Sessions {
		if top > 0 && i >= top {
			fmt.Fprintf(w, "\n（残り %d 件は --top で増やす）\n", len(s.Sessions)-top)
			break
		}
		row := []string{x.ID, fmt.Sprintf("%.1fs", x.StartSec), fmt.Sprintf("%.1fs", x.EndSec),
			md.Int(x.Requests), md.Int(x.Errors), md.Int(x.S499), md.Sec(x.MaxReqSec), strings.Join(x.Hosts, " ")}
		for _, r := range x.Lifecycles {
			if r.Reached == 0 {
				row = append(row, "-")
			} else {
				row = append(row, fmt.Sprintf("%d:%s", r.Reached, r.LastStep))
			}
		}
		md.Row(w, row)
	}
	fmt.Fprintln(w)
}

func RenderSessionMarkdown(w io.Writer, d *SessionDetail) {
	s := d.Summary
	fmt.Fprintf(w, "# analyze session %s（%s）\n\n", s.ID, d.GroupId)
	fmt.Fprintf(w, "- %.1fs〜%.1fs、%d 要求、エラー %d、499 %d\n\n", s.StartSec, s.EndSec, s.Requests, s.Errors, s.S499)
	md.Row(w, []string{"t", "要求", "host", "status", "応答", "進んだ状態"})
	md.Sep(w, 6)
	for _, r := range d.Requests {
		md.Row(w, []string{fmt.Sprintf("%.3fs", r.OffsetSec), r.Method + " " + r.URI, r.Host,
			md.Int(r.Status), md.Sec(r.ReqTimeSec), strings.Join(r.Steps, ", ")})
	}
	fmt.Fprintln(w)
}

func RenderTransitionsMarkdown(w io.Writer, t *TransitionsResponse) {
	title := "全セッション"
	if t.Lifecycle != "" {
		title = "ライフサイクル " + t.Lifecycle + " に入ったセッション"
	}
	fmt.Fprintf(w, "# analyze transitions %s（%s、%d セッション、件数の多い順）\n\n", t.GroupId, title, t.Sessions)
	md.Row(w, []string{"from", "to", "件数", "from の次に占める割合", "間隔 p50"})
	md.Sep(w, 5)
	for _, x := range t.Transitions {
		gap := ""
		if x.To != transitionEnd && x.From != transitionStart {
			gap = md.Sec(x.GapP50Sec)
		}
		md.Row(w, []string{x.From, x.To, md.Int(x.Count), md.Pct(x.ShareOfFrom), gap})
	}
	fmt.Fprintln(w)
}
