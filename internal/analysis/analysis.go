package analysis

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/smsnk/pprotein/internal/dbquery"
)

type (
	// Input は1回の収集から集めた材料。
	Input struct {
		GroupId string
		// Origin は時系列の 0 秒（unix 秒）。GroupId の時刻 = 収集開始 = initialize の直後。
		Origin   float64
		Requests []Request
		// SkippedLines は時刻が読めず捨てた LTSV の行数。
		SkippedLines int
		Queries      map[string]*dbquery.QueryResult
		// Warnings は材料を集める段階で見つかった問題（収集中・収集失敗のエントリなど）。
		Warnings []string

		// Score はこの収集に紐づいたベンチのスコア。無ければ nil。
		Score *ScoreInfo
		// ConfigHash は集計に使った設定のハッシュ。走行間の比較で定義の食い違いを見つけるのに使う。
		ConfigHash ConfigHash
		// Commit は httplog に記録されたアプリのコミット。無ければ nil。
		Commit *Commit
	}

	Commit struct {
		Hash    string
		Message string
		Ref     string `json:",omitempty"`
	}

	ScoreInfo struct {
		Value  int64
		Passed bool
	}

	// ConfigHash の各値は設定ファイルの中身の SHA-256 の先頭12桁。取れなかったものは空。
	ConfigHash struct {
		// Analysis は集計したときの analysis.yml。
		Analysis string
		// DBQuery は dbquery を実行したときの dbquery.yml（dbquery の結果に記録されている）。
		DBQuery string
	}

	// JSON のキーには単位を付ける（…Sec は秒、Rate… / Share は 0〜1 の割合）。
	// AI がドキュメントを読まずに解釈できるようにするため。
	Report struct {
		GroupId    string
		Origin     time.Time
		Bucket     int        `json:"BucketSec"`
		Warnings   []string   `json:",omitempty"`
		Score      *ScoreInfo `json:",omitempty"`
		Commit     *Commit    `json:",omitempty"`
		ConfigHash ConfigHash

		// Totals は走行全体のリクエストの集計（収集開始より後のもの）。
		Totals Totals
		// SeriesStats は時系列の系列ごとの集計（累積の系列は除く）。
		SeriesStats []SeriesStat

		Timeline   Timeline
		Lifecycles []LifecycleResult
		Breakdowns []BreakdownResult
	}

	Timeline struct {
		// Series は Buckets[].Series に入っている系列名（表示順）。
		Series []string
		// Endpoints は件数の多いエンドポイント（Buckets[].Endpoints のキー）。
		Endpoints []string
		Buckets   []Bucket
	}

	Totals struct {
		Requests int
		S2xx     int
		S3xx     int
		S4xx     int
		S5xx     int
		Canceled int     `json:"S499"`
		P50      float64 `json:"P50Sec"`
		P99      float64 `json:"P99Sec"`
		// PeakRequests は区間あたりの最大リクエスト数、PeakOffset はその区間の開始。
		PeakRequests int
		PeakOffset   int `json:"PeakOffsetSec"`
		// Endpoints は全エンドポイントの件数（Timeline.Endpoints と違って上位に絞らない）。
		Endpoints map[string]int `json:",omitempty"`
	}

	SeriesStat struct {
		Name string
		// Role は系列の役割（score: 得点の項 / driver: 得点を生む母数 / penalty: 得点を減らすもの）。未設定なら空。
		Role string `json:",omitempty"`
		// Query は元の dbquery の name。ライフサイクルの開始・完了数は lifecycle:<name>。
		Query string `json:",omitempty"`
		// Agg は区間内の集約方法（sum / count / max / last）。ライフサイクルの開始・完了数は count。
		Agg string
		// Total は走行全体の値。sum / count は合計、max は最大、last は最後の区間の値。
		Total float64
		// T50 / T90 は sum / count の系列が合計の 50% / 90% に達した区間の終わり（収集開始からの秒）。
		// 得点が前倒しで積み上がるようになったか（終盤の失速が直ったか）を見る。
		T50 *int `json:"T50Sec,omitempty"`
		T90 *int `json:"T90Sec,omitempty"`
	}

	Bucket struct {
		// Offset は区間の開始（Origin からの秒）。
		Offset   int `json:"OffsetSec"`
		Requests int
		S2xx     int
		S3xx     int
		S4xx     int
		S5xx     int
		// Canceled は 499（クライアントが待ちきれずに切った）。S4xx には入れない。
		Canceled  int                `json:"S499"`
		P50       float64            `json:"P50Sec"`
		P99       float64            `json:"P99Sec"`
		Endpoints map[string]int     `json:",omitempty"`
		Series    map[string]float64 `json:",omitempty"`
	}

	LifecycleResult struct {
		Name string
		// Entities は最初の状態に到達したエンティティの数。
		Entities int
		Steps    []StepResult
		Error    string `json:",omitempty"`
	}

	StepResult struct {
		Name string
		// Reached はこの状態まで順に到達したエンティティの数。
		Reached int
		// Rate は1つ前の状態に到達したうち、この状態まで進んだ割合。
		Rate float64 `json:"RateFromPrev"`
		// StoppedHere はこの状態を最後に先へ進まなかったエンティティの数（離脱・途中で走行終了の両方を含む）。
		StoppedHere int
		// PerEntity は到達したエンティティ1つあたり、この状態のイベントが何回あったか（順序を問わない）。
		PerEntity float64
		// FromPrev は1つ前の状態からの所要秒数。
		FromPrev *Quantiles `json:"FromPrevSec,omitempty"`
	}

	Quantiles struct {
		P50 float64
		P90 float64
		P99 float64
		Max float64
	}

	BreakdownResult struct {
		Name    string
		Columns []string
		Rows    [][]string
		// Share は value 列があるときの構成比（Rows と同じ並び）。
		Share     []float64 `json:",omitempty"`
		TotalRows int
		Error     string `json:",omitempty"`
	}
)

// Analyze は材料から Report を作る。
func Analyze(cfg *Config, in *Input) *Report {
	rep := &Report{
		GroupId: in.GroupId,
		Origin:  time.Unix(0, int64(in.Origin*1e9)),
		Bucket:  cfg.Bucket,

		Score:      in.Score,
		Commit:     in.Commit,
		ConfigHash: in.ConfigHash,
	}
	rep.Warnings = append(rep.Warnings, in.Warnings...)
	if in.SkippedLines > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("httplog: 時刻が読めない行を %d 行飛ばした", in.SkippedLines))
	}

	tl := newTimelineBuilder(cfg, in.Origin)
	tl.src = "httplog"
	for i := range in.Requests {
		tl.addRequest(&in.Requests[i])
	}

	for _, def := range cfg.Series {
		q, err := lookupQuery(in, def.Query)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("series %s: %v", def.Name, err))
			continue
		}
		tl.src = "series " + def.Name
		if err := tl.addSeries(def, q); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("series %s: %v", def.Name, err))
		}
	}

	for _, def := range cfg.Lifecycles {
		res, events := analyzeLifecycle(def, in)
		tl.src = "lifecycle " + def.Name
		rep.Lifecycles = append(rep.Lifecycles, res)
		for name, ts := range events {
			tl.addPoints(def.Name+"."+name, ts)
		}
	}

	for _, def := range cfg.Breakdowns {
		rep.Breakdowns = append(rep.Breakdowns, breakdown(def, in, cfg.Top))
	}

	rep.Timeline = tl.build(cfg.Top)
	rep.Totals = totals(in, rep.Timeline)
	rep.SeriesStats = seriesStats(rep.Timeline, tl.meta, cfg.Bucket)
	roundReport(rep)
	srcs := make([]string, 0, len(tl.before))
	for src := range tl.before {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		hint := "SQL の {{since}} 条件を確認"
		if src == "httplog" {
			hint = "収集開始の直前に終わったリクエスト"
		}
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: 収集開始より前の時刻のデータを %d 件除外した（%s）", src, tl.before[src], hint))
	}
	return rep
}

func lookupQuery(in *Input, name string) (*dbquery.QueryResult, error) {
	q, ok := in.Queries[name]
	if !ok {
		return nil, fmt.Errorf("dbquery %q の結果が無い（dbquery.yml と、走行後に POST /api/dbquery したかを確認）", name)
	}
	if q.Error != "" {
		return nil, fmt.Errorf("dbquery %q が失敗している: %s", name, q.Error)
	}
	return q, nil
}

// --- 時系列 ---

type timelineBuilder struct {
	bucket int
	origin float64

	buckets map[int]*bucketAcc
	series  []string
	seen    map[string]bool
	// before は Origin より前で捨てた件数（出どころ別）。src は今足しているデータの出どころ。
	before map[string]int
	src    string
	// meta は系列名ごとの集約方法・役割・元のクエリ。
	meta map[string]seriesMeta
}

type seriesMeta struct {
	agg, role, query string
}

type bucketAcc struct {
	Bucket
	reqTimes  []float64
	endpoints map[string]int
	series    map[string]float64
	lastT     map[string]float64
}

func newTimelineBuilder(cfg *Config, origin float64) *timelineBuilder {
	return &timelineBuilder{
		bucket:  cfg.Bucket,
		origin:  origin,
		buckets: map[int]*bucketAcc{},
		seen:    map[string]bool{},
		before:  map[string]int{},
		meta:    map[string]seriesMeta{},
	}
}

func (b *timelineBuilder) at(t float64) *bucketAcc {
	off := t - b.origin
	if off < 0 {
		b.before[b.src]++
		return nil
	}
	idx := int(off) / b.bucket
	acc, ok := b.buckets[idx]
	if !ok {
		acc = &bucketAcc{
			endpoints: map[string]int{},
			series:    map[string]float64{},
			lastT:     map[string]float64{},
		}
		acc.Offset = idx * b.bucket
		b.buckets[idx] = acc
	}
	return acc
}

func (b *timelineBuilder) declare(name string) {
	if !b.seen[name] {
		b.seen[name] = true
		b.series = append(b.series, name)
	}
}

func (b *timelineBuilder) addRequest(r *Request) {
	acc := b.at(r.T)
	if acc == nil {
		return
	}
	acc.Requests++
	switch {
	case r.Status == 499:
		acc.Canceled++
	case r.Status >= 500:
		acc.S5xx++
	case r.Status >= 400:
		acc.S4xx++
	case r.Status >= 300:
		acc.S3xx++
	case r.Status >= 200:
		acc.S2xx++
	}
	acc.reqTimes = append(acc.reqTimes, r.ReqTime)
	acc.endpoints[r.Key()]++
}

func (b *timelineBuilder) addSeries(def SeriesDef, q *dbquery.QueryResult) error {
	ti, vi, si := colIndex(q.Columns, "t"), colIndex(q.Columns, "value"), colIndex(q.Columns, "series")
	if ti < 0 {
		return fmt.Errorf("dbquery %q に t 列が無い", def.Query)
	}
	if vi < 0 && def.Agg != "count" {
		return fmt.Errorf("dbquery %q に value 列が無い（件数を数えるなら agg: count）", def.Query)
	}
	names := []string{}
	for _, row := range q.Rows {
		t, ok := cellFloat(row, ti)
		if !ok {
			continue
		}
		t = NormalizeUnixTime(t)
		name := def.Name
		if si >= 0 && row[si] != nil {
			name = def.Name + ":" + *row[si]
		}
		if !b.seen[name] {
			names = append(names, name)
		}
		b.declare(name)
		b.meta[name] = seriesMeta{agg: def.Agg, role: def.Role, query: def.Query}
		acc := b.at(t)
		if acc == nil {
			continue
		}
		v, _ := cellFloat(row, vi)
		switch def.Agg {
		case "sum":
			acc.series[name] += v
		case "count":
			acc.series[name]++
		case "max":
			if cur, ok := acc.series[name]; !ok || v > cur {
				acc.series[name] = v
			}
		case "last":
			if t >= acc.lastT[name] {
				acc.lastT[name] = t
				acc.series[name] = v
			}
		}
	}
	if def.Cumulative {
		for _, n := range names {
			b.declare(n + "(累積)")
		}
	}
	return nil
}

// addPoints は時刻の列を件数の系列として足す（ライフサイクルの開始・完了数）。
func (b *timelineBuilder) addPoints(name string, ts []float64) {
	b.declare(name)
	b.meta[name] = seriesMeta{agg: "count", query: "lifecycle:" + strings.TrimSuffix(strings.TrimSuffix(name, ".started"), ".finished")}
	for _, t := range ts {
		if acc := b.at(t); acc != nil {
			acc.series[name]++
		}
	}
}

func (b *timelineBuilder) build(top int) Timeline {
	tl := Timeline{Series: b.series}
	if len(b.buckets) == 0 {
		return tl
	}

	maxIdx := 0
	totals := map[string]int{}
	for idx, acc := range b.buckets {
		if idx > maxIdx {
			maxIdx = idx
		}
		for k, v := range acc.endpoints {
			totals[k] += v
		}
	}
	tl.Endpoints = topKeys(totals, top)

	cum := map[string]float64{}
	for idx := 0; idx <= maxIdx; idx++ {
		acc, ok := b.buckets[idx]
		if !ok {
			acc = &bucketAcc{series: map[string]float64{}, endpoints: map[string]int{}}
			acc.Offset = idx * b.bucket
		}
		out := acc.Bucket
		if len(acc.reqTimes) > 0 {
			sort.Float64s(acc.reqTimes)
			out.P50 = quantile(acc.reqTimes, 0.5)
			out.P99 = quantile(acc.reqTimes, 0.99)
		}
		out.Endpoints = map[string]int{}
		for _, k := range tl.Endpoints {
			if v := acc.endpoints[k]; v > 0 {
				out.Endpoints[k] = v
			}
		}
		out.Series = map[string]float64{}
		for _, name := range b.series {
			if v, ok := acc.series[name]; ok {
				out.Series[name] = v
			}
			if b.seen[name+"(累積)"] {
				cum[name] += acc.series[name]
				out.Series[name+"(累積)"] = cum[name]
			}
		}
		tl.Buckets = append(tl.Buckets, out)
	}
	return tl
}

// totals は走行全体のリクエストを集計する。時系列と同じく収集開始より前のものは除く。
func totals(in *Input, tl Timeline) Totals {
	t := Totals{Endpoints: map[string]int{}}
	times := []float64{}
	for i := range in.Requests {
		r := &in.Requests[i]
		if r.T < in.Origin {
			continue
		}
		times = append(times, r.ReqTime)
		t.Endpoints[r.Key()]++
	}
	for _, b := range tl.Buckets {
		t.Requests += b.Requests
		t.S2xx += b.S2xx
		t.S3xx += b.S3xx
		t.S4xx += b.S4xx
		t.S5xx += b.S5xx
		t.Canceled += b.Canceled
		if b.Requests > t.PeakRequests {
			t.PeakRequests = b.Requests
			t.PeakOffset = b.Offset
		}
	}
	if len(times) > 0 {
		sort.Float64s(times)
		t.P50 = quantile(times, 0.5)
		t.P99 = quantile(times, 0.99)
	}
	return t
}

// seriesStats は系列ごとの走行全体の値と、合計の 50% / 90% に達した時刻を出す。
func seriesStats(tl Timeline, metas map[string]seriesMeta, bucket int) []SeriesStat {
	stats := []SeriesStat{}
	for _, name := range tl.Series {
		m, ok := metas[name]
		if !ok {
			continue // 累積の系列は元の系列から導けるので出さない
		}
		st := SeriesStat{Name: name, Role: m.role, Query: m.query, Agg: m.agg}
		switch m.agg {
		case "sum", "count":
			for _, b := range tl.Buckets {
				st.Total += b.Series[name]
			}
			if st.Total > 0 {
				var cum float64
				for _, b := range tl.Buckets {
					cum += b.Series[name]
					end := b.Offset + bucket
					if st.T50 == nil && cum >= st.Total*0.5 {
						st.T50 = &end
					}
					if st.T90 == nil && cum >= st.Total*0.9 {
						st.T90 = &end
						break
					}
				}
			}
		case "max":
			found := false
			for _, b := range tl.Buckets {
				if v, ok := b.Series[name]; ok && (!found || v > st.Total) {
					st.Total = v
					found = true
				}
			}
		case "last":
			for _, b := range tl.Buckets {
				if v, ok := b.Series[name]; ok {
					st.Total = v
				}
			}
		}
		stats = append(stats, st)
	}
	return stats
}

// roundReport は JSON に出す小数を丸める。float32 由来の 0.03299999237060547 のような桁は
// 情報を持たずにトークンだけ食うので、秒はミリ秒、割合は 0.01% の単位にする。
func roundReport(rep *Report) {
	ms := func(v *float64) { *v = math.Round(*v*1000) / 1000 }
	ratio := func(v *float64) { *v = math.Round(*v*1e4) / 1e4 }
	ms(&rep.Totals.P50)
	ms(&rep.Totals.P99)
	for i := range rep.Timeline.Buckets {
		b := &rep.Timeline.Buckets[i]
		ms(&b.P50)
		ms(&b.P99)
		for k, v := range b.Series {
			b.Series[k] = math.Round(v*1000) / 1000
		}
	}
	for i := range rep.SeriesStats {
		ms(&rep.SeriesStats[i].Total)
	}
	for i := range rep.Lifecycles {
		for j := range rep.Lifecycles[i].Steps {
			st := &rep.Lifecycles[i].Steps[j]
			ratio(&st.Rate)
			st.PerEntity = math.Round(st.PerEntity*100) / 100
			if q := st.FromPrev; q != nil {
				ms(&q.P50)
				ms(&q.P90)
				ms(&q.P99)
				ms(&q.Max)
			}
		}
	}
	for i := range rep.Breakdowns {
		for j := range rep.Breakdowns[i].Share {
			ratio(&rep.Breakdowns[i].Share[j])
		}
	}
}

// --- ライフサイクル ---

type event struct {
	entity string
	state  string
	t      float64
}

// analyzeLifecycle は集計結果と、時系列に載せる時刻の列（開始・完了）を返す。
func analyzeLifecycle(def LifecycleDef, in *Input) (LifecycleResult, map[string][]float64) {
	res := LifecycleResult{Name: def.Name}

	var events []event
	if def.Source == "httplog" {
		for i := range in.Requests {
			r := &in.Requests[i]
			if r.Entity == "" {
				continue
			}
			events = append(events, event{r.Entity, r.Key(), r.T})
		}
	} else {
		q, err := lookupQuery(in, def.queryName())
		if err != nil {
			res.Error = err.Error()
			return res, nil
		}
		ei, si, ti := colIndex(q.Columns, "entity"), colIndex(q.Columns, "state"), colIndex(q.Columns, "t")
		if ei < 0 || si < 0 || ti < 0 {
			res.Error = fmt.Sprintf("dbquery %q には entity, state, t の列が要る", def.queryName())
			return res, nil
		}
		for _, row := range q.Rows {
			t, ok := cellFloat(row, ti)
			if !ok || row[ei] == nil || row[si] == nil {
				continue
			}
			events = append(events, event{*row[ei], *row[si], NormalizeUnixTime(t)})
		}
	}

	byEntity := map[string][]event{}
	for _, ev := range events {
		byEntity[ev.entity] = append(byEntity[ev.entity], ev)
	}

	n := len(def.Steps)
	reached := make([]int, n)
	stopped := make([]int, n)
	occ := make([]int, n)
	durs := make([][]float64, n)
	var starts, finishes []float64

	for _, evs := range byEntity {
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].t < evs[j].t })
		cursor := 0
		times := make([]float64, n)
		counts := make([]int, n)
		for _, ev := range evs {
			for j := range def.Steps {
				if def.Steps[j].re.MatchString(ev.state) {
					counts[j]++
				}
			}
			if cursor < n && def.Steps[cursor].re.MatchString(ev.state) {
				times[cursor] = ev.t
				cursor++
			}
		}
		if cursor == 0 {
			continue
		}
		res.Entities++
		starts = append(starts, times[0])
		if cursor == n {
			finishes = append(finishes, times[n-1])
		}
		stopped[cursor-1]++
		for j := 0; j < cursor; j++ {
			reached[j]++
			occ[j] += counts[j]
			if j > 0 {
				durs[j] = append(durs[j], times[j]-times[j-1])
			}
		}
	}

	for j, st := range def.Steps {
		sr := StepResult{Name: st.Name, Reached: reached[j], StoppedHere: stopped[j]}
		if j == 0 {
			if reached[0] > 0 {
				sr.Rate = 1
			}
		} else if reached[j-1] > 0 {
			sr.Rate = float64(reached[j]) / float64(reached[j-1])
		}
		if reached[j] > 0 {
			sr.PerEntity = float64(occ[j]) / float64(reached[j])
		}
		if len(durs[j]) > 0 {
			sort.Float64s(durs[j])
			sr.FromPrev = &Quantiles{
				P50: quantile(durs[j], 0.5),
				P90: quantile(durs[j], 0.9),
				P99: quantile(durs[j], 0.99),
				Max: durs[j][len(durs[j])-1],
			}
		}
		res.Steps = append(res.Steps, sr)
	}
	return res, map[string][]float64{"started": starts, "finished": finishes}
}

// --- 内訳 ---

func breakdown(def BreakdownDef, in *Input, top int) BreakdownResult {
	res := BreakdownResult{Name: def.Name}
	q, err := lookupQuery(in, def.Query)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Columns = q.Columns
	res.TotalRows = len(q.Rows)

	vi := colIndex(q.Columns, "value")
	var total float64
	if vi >= 0 {
		for _, row := range q.Rows {
			v, _ := cellFloat(row, vi)
			total += v
		}
	}
	for i, row := range q.Rows {
		if i >= top {
			break
		}
		cells := make([]string, len(row))
		for j, c := range row {
			if c != nil {
				cells[j] = *c
			}
		}
		res.Rows = append(res.Rows, cells)
		if vi >= 0 && total != 0 {
			v, _ := cellFloat(row, vi)
			res.Share = append(res.Share, v/total)
		}
	}
	return res
}

// --- helpers ---

func colIndex(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func cellFloat(row []*string, i int) (float64, bool) {
	if i < 0 || i >= len(row) || row[i] == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(*row[i], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// quantile は昇順に並んだ値の nearest-rank 分位点。
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func topKeys(m map[string]int, n int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if n > 0 && len(keys) > n {
		keys = keys[:n]
	}
	return keys
}
