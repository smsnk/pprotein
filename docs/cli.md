# pprotein-cli

計測結果をテキストで取り出す CLI。AI エージェントに渡すことを想定している。

UI は HTML なのでエージェントからは読めず、API は生 JSON で情報密度が悪い。
`curl .../api/pprof` を叩いて python でパースする手順を毎ラウンド書き直すのをやめる。

```sh
make pprotein-cli          # あるいは go build ./cli/pprotein-cli
```

接続先は `--url`（既定 `http://localhost:9000`、環境変数 `PPROTEIN_URL` でも指定できる）。
observer に置いてもよいし、手元から API 越しに叩いてもよい。

コマンドとフラグの一覧は [リファレンス](cli/pprotein-cli.md)。コマンドの定義から `make docs` で作っている
（`pprotein-cli <command> --help` でも同じものが出る）。

## どのコマンドを使うか

系統が2つあり、答える問いが違う。

| | `summary` 系 | `analyze` 系 |
| --- | --- | --- |
| 答える問い | サーバーの中で何が遅いか | 得点がいつどこで生まれ、利用者がどこまで進んだか |
| 材料 | alp / slp の集計、pprof、resource、memo | httplog の生ログ、dbquery の SQL の結果、score |
| 集計する場所 | CLI | サーバー（`/api/analysis`、キャッシュあり） |
| サーバーの単位 | label（isu1, isu2, …）ごとに別の表 | 全 label をまとめる（`analysis.yml` の `httplog.label` で1台に絞れる） |
| 2回の比較 | `summary diff`: 1件あたりの時間の増減（IMPACT）の大きい順 | `analyze diff`: 系列・リクエスト・エンドポイント構成比・ライフサイクル・内訳 |

| コマンド | 内容 |
| --- | --- |
| `collect` | 収集を開始して完了まで待ち、そのまま `summary` を出す |
| `groups` | 収集の一覧（新しい順。コミット・スコア付き） |
| `summary [GROUP]` | 1回の収集を要約する |
| `summary diff BEFORE [AFTER]` | 2つの収集の httplog / slowlog を比べる |
| `analyze [GROUP]` | 1回の走行を時系列・ライフサイクル・内訳で集計する（[analysis.md](analysis.md)） |
| `analyze diff BEFORE [AFTER]` | 2つの走行の analyze を比べる |
| `analyze trend` | 直近の走行を1行ずつ並べる（得点・母数・利用者の進み方の推移） |
| `analyze endpoints [GROUP]` | エンドポイント別の件数・構成比・p50・p99・合計・エラー・499（全件） |
| `analyze sessions [GROUP]` | セッション（利用者）の一覧 |
| `analyze session GROUP SESSION_ID` | 1セッションの要求の列 |
| `analyze transitions [GROUP]` | よくある遷移（ある要求の次に叩くエンドポイント） |

`GROUP` は収集の GroupId（`2025-11-23_10-41-02.984213` の形）。省略すると `latest` で、
収集中のものも含め、全部が失敗した収集を除いた最新の収集を指す（走行直後に1つ前の走行を指すと、取り違えに気づけない）。
全部が失敗した新しい収集を飛ばしたときは、stderr にそう書く。
どのコマンドでも同じ意味で、`diff` の BEFORE / AFTER にも書ける（AFTER を省くと latest と比べる）。

## フラグ

フラグはコマンドごとに決まっている。そのコマンドに無いフラグはエラーになる。

| フラグ | 使えるコマンド | 内容 |
| --- | --- | --- |
| `--url` / `--json` / `--timeout D` | 全部 | 接続先 / JSON で出す / API の応答と完了待ちの上限（既定 5m） |
| `--top N` | `summary`、`summary diff`、`collect`、`analyze diff` | 各表の行数（既定 10、0 で全部）。`analyze diff` はエンドポイント構成比の表だけ |
| `--limit N` | `groups`、`analyze trend`、`analyze sessions`、`analyze transitions` | 出す収集・走行・セッション・遷移の数（`--json` にも効く）。`groups` と `analyze sessions` は 0 で全部、`analyze trend` は 1〜100 |
| `--wait` | `summary`、`analyze` | その収集のうち、そのコマンドが読むエントリが確定するまで待ってから出す（`collect` は全部のタイプを待つ） |
| `--no-wait` | `collect` | 収集を開始するだけで、完了を待たない |
| `--lifecycle NAME` | `analyze sessions`、`analyze transitions` | そのライフサイクルに入ったセッションに絞る |

フラグは GNU 形式（`--top 5`）で、`-top 5` は通らない。

## 出力

Markdown は見出し・箇条書き・表だけで書く。見出しはコマンド名で、`summary` / `analyze` とその `diff` は、その下に収集の素性（スコア・コミット・警告）を並べる。
時間の値には単位（`s`）が付く。

`summary`:

````markdown
# summary 2025-11-23_10-41-02.984213

- 収集: 2025-11-23 10:41:02
- スコア: 18902
- コミット: a1b2c3d "notification のキャッシュ" (refs/heads/12-cache-notification)

## httplog: isu1（SUM の大きい順に 10 件）

| COUNT | METHOD | URI | SUM | AVG | P99 |
| --- | --- | --- | --- | --- | --- |
| 4200 | GET | /api/app/notification | 8.400s | 0.002s | 0.011s |

## slowlog: isu1（合計時間の大きい順に 10 件）

| COUNT | SUM | AVG | ROWS EXAMINED (avg) | QUERY |
| --- | --- | --- | --- | --- |
| 1 | 0.900s | 0.900s | 900000 | SELECT COUNT(N) FROM `chairs` WHERE `is_active` = N |

## pprof: isu1（flat の大きい順に 10 件）

| FLAT | FLAT% | CUM | FUNCTION |
| --- | --- | --- | --- |
| 1.50s | 32.1% | 2.10s | encoding/json.(*encodeState).marshal |

## resource: isu1

```
host: isu1 (4 cpu)
[host cpu %]
busy 92.4 ...
```
````

`summary diff` は IMPACT の大きい順に並べる。IMPACT は「件数を揃えたとき、合計でどれだけ時間が増減したか」で、
1件あたりの増減に前後の件数の平均を掛けたもの。固定時間のベンチでは、速くしても空いた時間に次のリクエストが入るので
SUM はほとんど動かない。SUM の増減で並べると、いちばん効いた施策が下に埋もれる。
片方にしか無い行は（なし）と書き、その SUM がまるごと増えた（追加）/ 減った（削除）ものとして数える。

```markdown
# summary diff 2025-11-23_10-21-33.123456 -> 2025-11-23_10-41-02.984213

- スコア: 12345 -> 18902 (+6557, +53.1%)
- コミット: 9f8e7d6 "rides のインデックス" (refs/heads/11-index-rides) -> a1b2c3d "notification のキャッシュ" (refs/heads/12-cache-notification)

## httplog（IMPACT の大きい順）

| LABEL | ENDPOINT | COUNT | AVG | SUM | IMPACT |
| --- | --- | --- | --- | --- | --- |
| isu1 | GET /api/app/notification | 4188 → 4200 | 0.015s → 0.002s | 62.820s → 8.400s | -54.522s |
| isu1 | POST /api/app/rides | 300 → 300 | 0.014s → 0.033s | 4.100s → 9.800s | +5.700s |
| isu1 | GET /api/chair/notification | （なし） → 120 | （なし） → 0.010s | （なし） → 1.200s | +1.200s（追加） |
```

`collect` はベンチ走行から結果取得までを1コマンドにする。
「`Duration` 秒待ってからステータスを確認する」を手でやらずに済む。

```sh
pprotein-cli collect --top 5
# 収集を開始しました
# 収集が確定しました: 2025-11-23_10-41-02.984213
# （そのまま summary が出る）
```

`--json` は詰めた1行の JSON（インデントすると AI が読むときのトークンが倍以上になる）。

- キーには単位が付く。`…Sec` は秒、`…Share` は 0〜1 の割合。pprof の `Flat` / `Cum` だけは単位を `Unit` に持つ（CPU プロファイルなら `seconds`）
- 収集の素性は summary 系・analyze 系で同じ形: `GroupId`、`Score`（`Value` / `Passed`）、`Commit`（`Hash` / `Message` / `Ref`）、`Warnings`
- `diff` はスコア・各行を `Before` / `After` で持つ（`summary diff` はコミットも）。片方にしか無い行はもう片方が無い（追加・削除）
- analyze 系の中身は [analysis.md](analysis.md) の「AI に読ませる（JSON）」

## メモ

- 収集がまだ終わっていない / 失敗した / 中身を読めなかったエントリは、見出しの下に **警告** として出る。警告が出ている節は欠けている
- 出力はトークン効率を意識して既定で各表 10 行に絞ってある。長いクエリは1行に潰して切り詰める
- alp / slp の列名は実出力に合わせてあるが、設定で列が変わっても**ヘッダ名で引く**ので壊れにくい
- 古い名前（`latest`、`diff`、`analyze-diff`、`analyze-trend`、`endpoints`、`sessions`、`session`、`transitions`）も使える。
  help には出ず、使うと新しい名前を stderr に案内する。変わったフラグは、`sessions` / `transitions` の `--top` が `--limit`、
  `collect --wait=false` が `--no-wait`。`-top` のような `-` 1つの長いフラグは通らない
