package resource

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/goccy/go-json"
)

// DefaultInterval はサンプリング間隔。
// 常駐して走行中に動くものなので、粗くしてオーバーヘッドを抑える。
// PPROTEIN_RESOURCE_INTERVAL（秒）で変えられる。
const DefaultInterval = 5 * time.Second

// NewHandler は /debug/resource のハンドラを返す。
//
//	GET /debug/resource?seconds=90[&interval=5]
//
// seconds の間サンプリングし、Report の JSON を返す。
// pprotein の collector が Duration を seconds として付けてくるので、
// 他の計測と同じ収集ライフサイクル（開始トリガー / Duration）にそのまま乗る。
func NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := queryDuration(r, "seconds", 30*time.Second)
		interval := queryDuration(r, "interval", envInterval())

		report, err := Collect(duration, interval)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			// ここまで来たらヘッダは送信済みなので、記録だけして諦める
			return
		}
	})
}

func queryDuration(r *http.Request, key string, def time.Duration) time.Duration {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	sec, err := strconv.ParseFloat(v, 64)
	if err != nil || sec <= 0 {
		return def
	}
	return time.Duration(sec * float64(time.Second))
}

func envInterval() time.Duration {
	v := os.Getenv("PPROTEIN_RESOURCE_INTERVAL")
	if v == "" {
		return DefaultInterval
	}
	sec, err := strconv.ParseFloat(v, 64)
	if err != nil || sec <= 0 {
		return DefaultInterval
	}
	return time.Duration(sec * float64(time.Second))
}
