# pprotein-cli

計測結果をテキストで取り出す CLI。AI エージェントに渡すことを想定している。

UI は HTML なのでエージェントからは読めず、API は生 JSON で情報密度が悪い。
`curl .../api/pprof` を叩いて python でパースする手順を毎ラウンド書き直すのをやめる。

```sh
make pprotein-cli          # あるいは go build ./cli/pprotein-cli
```

接続先は `--url`（既定 `http://localhost:9000`、環境変数 `PPROTEIN_URL` でも指定できる）。
observer に置いてもよいし、手元から API 越しに叩いてもよい。

## コマンド

| コマンド | 内容 |
| --- | --- |
| `latest` | 直近の収集を要約する |
| `summary <groupId>` | 指定した収集を要約する |
| `groups` | 収集の一覧（スコア・コミット付き） |
| `diff <before> <after>` | 2つの収集を比較する |
| `collect` | 収集を開始して完了まで待ち、そのまま要約する |

共通フラグ: `--top N`（各表の件数、既定 10）、`--json`、`--url`。

## 出力

`latest` はそのままエージェントの文脈に貼れる Markdown を出す。

```markdown
# group 2025-11-23_10-41-02.984213

- collected: 2025-11-23 10:41:02
- commit: a1b2c3d "notification のキャッシュ" (refs/heads/12-cache-notification)
- score: score=18902 pass errors=3 target=isu1 120s

## httplog: isu1 (top 10 by SUM)

| COUNT | METHOD | URI | SUM | AVG | P99 |
| ---: | --- | --- | ---: | ---: | ---: |
| 4200 | GET | /api/app/notification | 62.310 | 0.015 | 0.082 |

## slowlog: isu1 (top 10 by total query time)

| COUNT | SUM | AVG | ROWS EXAMINED (avg) | QUERY |
| ---: | ---: | ---: | ---: | --- |
| 1 | 0.900 | 0.900 | 900000 | SELECT COUNT(N) FROM `chairs` WHERE `is_active` = N |

## pprof: isu1 (top 10 by flat)

| FLAT | FLAT% | CUM | FUNCTION |
| ---: | ---: | ---: | --- |
| 1.50s | 32.1% | 2.10s | encoding/json.(*encodeState).marshal |

## resource: isu1

```
host: isu1 (4 cpu)
[host cpu %]
busy 92.4 ...
```
```

`diff` は増減の大きい順に並べる。**何が改善して何が悪化したか**が上から読める。

```markdown
# diff 2025-11-23_10-21-33.123456 -> 2025-11-23_10-41-02.984213

- score: 12345 -> 18902 (+6557, +53.1%)

## httplog（SUM の増減が大きい順）

| LABEL | ENDPOINT | SUM before | SUM after | DELTA | COUNT delta |
| --- | --- | ---: | ---: | ---: | ---: |
| isu1 | GET /api/app/notification | 62.310 | 8.200 | -54.110 | +12 |
| isu1 | POST /api/app/rides | 4.100 | 9.800 | +5.700 | 0 |
```

`collect` はベンチ走行から結果取得までを1コマンドにする。
「`Duration` 秒待ってからステータスを確認する」を手でやらずに済む。

```sh
pprotein-cli collect --top 5
# 収集を開始しました
# 収集が完了しました: 2025-11-23_10-41-02.984213
# （そのまま要約が出る）
```

## メモ

- 出力はトークン効率を意識して既定で上位10件に絞ってある。長いクエリは1行に潰して切り詰める
- `--json` で構造化データが出る。スクリプトから使うときはこちら
- 収集がまだ終わっていない / 失敗しているエントリは、要約の先頭に **収集中** / **失敗** として出る
- alp / slp の列名は実出力に合わせてあるが、設定で列が変わっても**ヘッダ名で引く**ので壊れにくい
