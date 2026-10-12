# 走行の得点内訳と利用者の行動（dbquery / analysis）

alp / slow log / pprof は「サーバーの中で何が遅いか」を教えてくれるが、
「ベンチの利用者が何をしていて、得点がいつ・どこで生まれたか」は教えてくれない。
ISUCON では alp の上位がスコアに1点も寄与しないことが普通にあるので、
**何を増やすと得点が伸びるか**を走行ごとに見えるようにする。

| 部品 | 役割 |
| --- | --- |
| `dbquery` | 走行直後にアプリの DB へ SQL を流し、結果を収集グループに紐づけて保存する。次の initialize で消える行を残すため |
| `analysis` | 1つの収集グループについて、httplog の生ログと dbquery の結果を時刻で揃えて集計する |
| `pprotein-cli analyze` | analysis を Markdown で出す（エージェント向け） |
| `pprotein-cli analyze-diff` | 2つの走行の analysis を比べる（施策の前後で何が変わったか） |
| `pprotein-cli analyze-trend` | 直近の走行を1行ずつ並べる（施策を重ねた推移） |

回ごとの知識（どの SQL が得点か、利用者がどの順で進むか）は `dbquery.yml` と `analysis.yml` に置き、
コードには持たない。エンドポイントのまとめ方は `alp.yml` の `matching_groups` をそのまま使う。
どちらの設定も UI の設定画面（`#/setting/`）から編集できる。

## 出るもの

- **時系列**: 区間（既定5秒）ごとのリクエスト数・ステータス別件数（499 は別列）・p50/p99、
  SQL で取った系列（得点、母集団の増減など）、ライフサイクルの開始・完了数。
  0 秒は GroupId の時刻（initialize からの収集開始）
- **エンドポイント別の件数**: 件数の多い順に、区間ごと
- **ライフサイクル**: エンティティ（利用者・ライドなど）が状態を順に進む様子。
  状態ごとの到達数・前の状態からの到達率・そこで止まった数・1件あたりの回数・前の状態からの所要時間
- **内訳**: SQL の結果表。`value` 列があれば構成比を付ける

## 人が見る（UI の analysis タブ）

| 画面 | URL | 内容 |
| --- | --- | --- |
| 走行の推移 | `#/analysis/` | 走行を横軸に、スコア・役割ごとの系列・ライフサイクルの開始数と最後の状態への到達数・リクエスト総数・エラー・応答時間。カーソルで全グラフの同じ走行に縦線が出る。下の一覧から詳細へ、2行を選ぶと比較へ。エンドポイントを選ぶと、件数・構成比・p99・エラーを走行ごとに並べる |
| 1走行 | `#/analysis/<GroupId>/` | 収集開始からの秒を横軸に、得点の累積と区間ごと・リクエスト・エラー・応答時間・上位のエンドポイント・役割ごとの系列・ライフサイクルの開始と完了。ライフサイクルは到達数の横棒（ファネル）、内訳は構成比の横棒付きの表。下に全エンドポイントの表（件数・構成比・p50・p99・合計・エラー・499）があり、チェックしたものの区間ごとの件数・p99・エラー・499 をグラフにする |
| 比較 | `#/analysis/diff/<A>/<B>/` | analyze-diff と同じ集計。増減は色で示す（penalty の系列・エラー・遅延は減るほど良い向き）。ライフサイクルは before / after の横棒を上下に並べる |
| セッション一覧 | `#/analysis/<GroupId>/sessions/` | よくある遷移（「この要求の次に何を叩いたか」の件数と割合）と、セッションの表（開始・長さ・要求数・エラー・499・最大応答・訪れた host・ライフサイクルでどこまで進んだか）。ライフサイクルと最後に到達した状態で絞れる |
| 1セッション | `#/analysis/<GroupId>/sessions/<ID>/` | 横軸に時刻、縦軸にエンドポイントを取った導線図（点は応答を返した時刻、横線は処理にかかった時間、色はステータス、ライフサイクルが進んだ要求は輪）と、要求の列 |

データはすべて下の JSON の API から取る（`GET /api/analysis`、`/api/analysis/<GroupId>`、`/api/analysis/diff?before=&after=`）。

## セッションの導線

ライフサイクルの集計は「何人がどこまで進んだか」しか出さない。止まった利用者が直前に何をしていたか、
ベンチのシナリオがどんな順でエンドポイントを叩くかは、1人ずつの列と遷移を見ないと分からない。
ベンチのソースが無い本番でも、ここからシナリオの形を推測できる。

| 読み込み口 | 内容 |
| --- | --- |
| `GET /api/analysis/<GroupId>/sessions[?lifecycle=<name>]` / `pprotein-cli sessions <GroupId>` | セッションの一覧 |
| `GET /api/analysis/<GroupId>/sessions/<ID>` / `pprotein-cli session <GroupId> <ID>` | 1セッションの要求の列（まとめる前の URI・host・ステータス・応答時間・進んだ状態） |
| `GET /api/analysis/<GroupId>/transitions[?lifecycle=<name>&top=N]` / `pprotein-cli transitions <GroupId>` | よくある遷移 |

- セッションは `httplog.entity_key`（cookie など）で束ねた利用者。対象は `source: httplog` のライフサイクルだけ
- セッションの ID は開始時刻順の連番（`s001`, `s002`, …）。同じ材料なら同じ番号になる
- ログイン要求は cookie を持たないので、各セッションはログインの直後から始まる
- nginx はログを要求の終わりに書くので、要求の時刻は「応答を返した時刻」。開始は 時刻 − 応答時間

## AI に読ませる（JSON）

Markdown は人が端末で読むためのもの。エージェントやスクリプトは JSON を読む。

| 読み込み口 | 内容 | 大きさの目安（ISUCON13 の1走行） |
| --- | --- | --- |
| `GET /api/analysis/<GroupId>`（`latest` 可）/ `pprotein-cli analyze <GroupId> --json` | 1走行の全部 | 7.6KB |
| `GET /api/analysis?limit=N` / `pprotein-cli analyze-trend --limit N --json` | 走行ごとの要約（古い順） | 1走行あたり約 2KB |
| `GET /api/analysis?limit=N&endpoints=1` | 走行ごとの要約に、全エンドポイントの集計（`Endpoints`）を足したもの | 1走行あたり約 5KB |
| `GET /api/analysis/<GroupId>/endpoints` | 全エンドポイントの区間ごとの件数・p50・p99・エラー・499 | 約 9KB |
| `pprotein-cli endpoints <GroupId> [--json]` | 全エンドポイントの走行全体の集計（`Report.EndpointStats`） | |
| `pprotein-cli analyze-diff <A> <B> --json` | 2走行の比較 | 6.9KB |

ドキュメントを読まなくても解釈できるようにしてある。

- **キーに単位が付いている。** `…Sec` は秒、`RateFromPrev` と `Share` は 0〜1 の割合、`S499` は 499 の件数
- **系列に役割と出どころが付いている。** `SeriesStats[].Role` が `score`（得点の項）/ `driver`（得点を生む母数）/
  `penalty`（得点を減らすもの）。`Query` は元の dbquery の name（ライフサイクルの開始・完了数は `lifecycle:<name>`）
- **時系列は列ごとの配列。** `Timeline.OffsetSec` と同じ並びで、`Requests` / `P99Sec` / `Series[].Values` /
  `Endpoints[].Values` が並ぶ。系列の値が無い区間は `null`
- 秒はミリ秒、割合は 0.01% の単位に丸めてある
- `Commit` はアプリのコミット（httplog に記録されたもの）、`ConfigHash` は集計に使った設定のハッシュ

走行の要約はサーバー側でキャッシュする（材料と設定が変わらなければ集計し直さない）。
収集中のエントリがある走行はキャッシュせず、`Warnings` に理由が入る。

## 走行を比べる（analyze-diff）

```sh
pprotein-cli analyze-diff <before の GroupId> <after の GroupId> [--top N] [--json]
```

系列・ライフサイクル・内訳は**名前で突き合わせる**だけで、何の系列かは知らない（中身は回ごとの設定で決まる）。

| 節 | 比べるもの | 何が分かるか |
| --- | --- | --- |
| 見出し | スコア、設定の食い違い | analysis.yml / dbquery.yml のハッシュが走行間で違えば警告する |
| 系列 | 走行全体の合計（max / last は最大・最後の値）、合計の 50% / 90% に達した時刻 | 得点や母集団の増減。得点が前倒しで積み上がるようになったか |
| リクエスト | 総数・4xx・5xx・499・p50・p99・区間あたりの最大 | 負荷が増えたか（速くするとベンチが攻めてくるか）。タイムアウトが減ったか |
| エンドポイント構成比 | 件数と比率。比率の差が大きい順 | アクセスパターンの変化。alp の diff は所要時間が中心なので、ここは構成で見る |
| ライフサイクル | 状態ごとの到達数・到達率・止まった数・1件あたり回数・所要 p50 / p90 | 利用者がどこまで進むようになったか。どの区間が速くなったか |
| 内訳 | key / value 列がある表は key ごとの差、上位1件・3件の構成比、新たに上位に入った key。無い表は行数だけ | 得点の偏りが変わったか |

- **2走行の比較では有意差を判定しない。** 同じコードで2本回すと、件数の少ない系列（ライフサイクルの完了数など）は
  ±25% 程度動き、内訳の上位も入れ替わる（ISUCON13 の素振りで実測）。合計の大きい系列・リクエスト総数・
  エンドポイント構成比は数%以内に収まった。判断に使う前に、同じコードの2本で各指標の揺れを見ておく
- 片方にしか無い系列・状態は「追加」「削除」として出し、差は計算しない
- 内訳は上位 N 件しか持っていないので、上位から外れた key の値は「上位外」になる

## 系列の選び方

`analyze-diff` が比べる系列は、競技開始直後に `analysis.yml` で決める。マニュアルから次の順に拾い、
`role` に書く（出力に載るだけで集計は変わらない）。

1. **得点の項（必須、`role: score`）**: スコア計算式の項ごとに1系列。項が複数なら `series` 列で分ける。合計がベンチのスコアと合うかで確かめる
2. **得点を生む母数（`role: driver`）**: 世界観の節で「増えると得点が伸びる」と書かれているもの（利用者・予約・椅子の数など）
3. **得点を減らすもの（`role: penalty`）**: 減点や、得点を下げると書かれているもの

2 と 3 はマニュアルの読みに依存するので外れることがある。合わせて 5〜8 本に抑える。
DB に得点の材料が無い回は、httplog のエンドポイント構成比とリクエストの合計が代わりになる（系列を書かなくても出る）。

## 抽象の選び方

回によって「得点」と「利用者の振る舞い」の形は違う。

| | ISUCON13 (ISUPipe) | ISUCON14 (ISURIDE) |
| --- | --- | --- |
| 得点 | 投げ銭の合計 | 移動距離 ×0.1 + 移動距離 + 完了数 ×5 |
| ライフサイクル | 視聴者のセッション（入室 → コメント取得 → 投稿）。途中で止まる = 離脱 | ライド（MATCHING → … → COMPLETED）。区間の所要時間が評価を決める |
| 母集団 | 視聴者・配信者の数 | 評価でユーザーが、収益で椅子が増える |

離脱（ファネル）は回に依存するので、汎用の部品は「状態を順に進むイベント列」と「時系列」と「表」にした。
得点が複数の項の和なら、SQL に `series` 列を付けて項ごとの系列にする。

## 設定

### dbquery.yml

```yaml
dsn: isucon:isucon@tcp(127.0.0.1:9012)/isupipe   # observer からはトンネル越し
timeout: 30s
queries:
  - name: tips
    sql: |
      SELECT created_at AS t, tip AS value
      FROM livecomments WHERE tip > 0 AND created_at > {{since}}
```

`{{since}}` / `{{until}}` は unix 秒に置換される（既定は GroupId の時刻〜実行時刻）。
**初期データの時刻が initialize の時刻で入る回がある**（ISUCON13 は `UNIX_TIMESTAMP()`）ので、
`>` で厳密に後の行だけを取る。

走行が終わったら、その GroupId を付けて叩く。次の走行の initialize より前に。

```sh
curl -X POST http://localhost:9000/api/dbquery -H 'Content-Type: application/json' \
  -d '{"GroupId": "2026-10-11_16-41-34.230854"}'
```

### analysis.yml

```yaml
bucket: 5
top: 10
httplog:
  time_key: msec          # nginx に msec:$msec を足す。無ければ time を見る
  entity_key: session     # nginx に session:$cookie_<名前> を足す
series:
  - {name: tip, query: tips, role: score, cumulative: true}   # 列 t, value[, series]
breakdowns:
  - {name: 配信者別, query: tips_by_streamer}          # 結果表をそのまま
lifecycles:
  - name: viewer
    source: httplog                                   # または query:<name>（列 entity, state, t）
    steps:
      - {name: enter, match: '^POST .*/enter[$]?$'}
      - {name: post,  match: '^POST .*/livecomment[$]?$'}
```

- `t` は秒・ミリ秒・マイクロ秒のどれでもよい（桁で判定する）。DATETIME は `UNIX_TIMESTAMP(col)` で渡す
- httplog の状態は `"METHOD <matching_groups でまとめた URI>"`。まとめた URI は正規表現の文字列なので、
  `match` は URI の末尾で当てると書きやすい
- **cookie で束ねるとき、cookie を発行する要求（ログイン）は識別子を持たない。** 起点はその後の要求にする

## 読み方の注意

- **DB から復元した得点はベンチのスコアの上限。** 負荷走行の終了でベンチが切った要求も、
  サーバー側でコミットされていれば DB に残る（ISUCON13 の素振りで、一致した走行と 100 点多い走行があった）
- ライフサイクルの「止まった」は離脱と、走行終了で途中だったものを区別しない。終盤の区間の開始数と合わせて読む
- httplog の Duration は、initialize から負荷走行の終わりまでを覆う長さにする（短いと終盤が欠ける）
- **走行直後は httplog がまだ収集中（pending）のことが多い。** httplog の収集は Duration 秒（例: 110 秒）経つまで終わらない。
  収集中・失敗したエントリは集計に含めず、「httplog isu1 は収集中（pending）」のように警告に出す。
  警告が出ていたら、ライフサイクルの 0 件やリクエスト数の 0 は実際の値ではない。完了を待ってから読み直す。
  `pprotein-cli analyze --wait [groupId]` は、その収集の httplog / dbquery に pending が無くなるまで待ってから出す（上限は `--timeout`）
- `latest` は収集中の httplog も含めて最新の収集を指す（走行直後に1つ前の走行と取り違えないため）
