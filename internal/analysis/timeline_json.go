package analysis

import (
	"encoding/json"
)

// Timeline の JSON は列ごとの配列にする（区間ごとのオブジェクトにすると、エンドポイント名・系列名の
// キーが区間の数だけ繰り返され、AI が読むときのトークンの大半がキーになる）。
// 値の並びは OffsetSec と同じ。系列の値が無い区間は null。
type timelineJSON struct {
	OffsetSec []int
	Requests  []int
	S2xx      []int
	S3xx      []int
	S4xx      []int
	S5xx      []int
	S499      []int
	P50Sec    []float64
	P99Sec    []float64
	Series    []namedFloats `json:",omitempty"`
	Endpoints []namedInts   `json:",omitempty"`
}

type namedFloats struct {
	Name   string
	Values []*float64
}

type namedInts struct {
	Name   string
	Values []int
}

func (tl Timeline) MarshalJSON() ([]byte, error) {
	n := len(tl.Buckets)
	j := timelineJSON{
		OffsetSec: make([]int, n), Requests: make([]int, n),
		S2xx: make([]int, n), S3xx: make([]int, n), S4xx: make([]int, n), S5xx: make([]int, n), S499: make([]int, n),
		P50Sec: make([]float64, n), P99Sec: make([]float64, n),
	}
	for i, b := range tl.Buckets {
		j.OffsetSec[i], j.Requests[i] = b.Offset, b.Requests
		j.S2xx[i], j.S3xx[i], j.S4xx[i], j.S5xx[i], j.S499[i] = b.S2xx, b.S3xx, b.S4xx, b.S5xx, b.Canceled
		j.P50Sec[i], j.P99Sec[i] = b.P50, b.P99
	}
	for _, name := range tl.Series {
		col := namedFloats{Name: name, Values: make([]*float64, n)}
		for i, b := range tl.Buckets {
			if v, ok := b.Series[name]; ok {
				col.Values[i] = &v
			}
		}
		j.Series = append(j.Series, col)
	}
	for _, key := range tl.Endpoints {
		col := namedInts{Name: key, Values: make([]int, n)}
		for i, b := range tl.Buckets {
			col.Values[i] = b.Endpoints[key]
		}
		j.Endpoints = append(j.Endpoints, col)
	}
	return json.Marshal(j)
}

func (tl *Timeline) UnmarshalJSON(data []byte) error {
	j := timelineJSON{}
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	at := func(s []int, i int) int {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	atF := func(s []float64, i int) float64 {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	*tl = Timeline{}
	for i, off := range j.OffsetSec {
		b := Bucket{
			Offset: off, Requests: at(j.Requests, i),
			S2xx: at(j.S2xx, i), S3xx: at(j.S3xx, i), S4xx: at(j.S4xx, i), S5xx: at(j.S5xx, i), Canceled: at(j.S499, i),
			P50: atF(j.P50Sec, i), P99: atF(j.P99Sec, i),
			Series: map[string]float64{}, Endpoints: map[string]int{},
		}
		for _, col := range j.Series {
			if i < len(col.Values) && col.Values[i] != nil {
				b.Series[col.Name] = *col.Values[i]
			}
		}
		for _, col := range j.Endpoints {
			if v := at(col.Values, i); v > 0 {
				b.Endpoints[col.Name] = v
			}
		}
		tl.Buckets = append(tl.Buckets, b)
	}
	for _, col := range j.Series {
		tl.Series = append(tl.Series, col.Name)
	}
	for _, col := range j.Endpoints {
		tl.Endpoints = append(tl.Endpoints, col.Name)
	}
	return nil
}
