# ベンチのスコアを収集に紐づける

ベンチのスコアはベンチ実行スクリプトの標準出力にしか無く、pprotein 側は関知していなかった。
そのため「この収集結果はスコアいくつの走行なのか」を人間が時刻で突き合わせる必要があった。

スコアを収集グループ(`GroupId`)に紐づけて1つの snapshot として保存できるようにした。

## 投げる

走行が終わったら、その走行に対応する `GroupId` を付けて POST する。

```sh
curl -X POST http://localhost:9000/api/score \
  -H 'Content-Type: application/json' \
  -d '{
        "GroupId": "2025-11-23_10-41-02.984213",
        "Score": 18902,
        "Passed": true,
        "Target": "isu1",
        "ErrorCount": 3,
        "StartedAt": "2025-11-23T10:41:02Z",
        "FinishedAt": "2025-11-23T10:43:02Z",
        "Raw": "{\"pass\":true,\"score\":18902}"
      }'
```

| フィールド | 必須 | 内容 |
| --- | --- | --- |
| `GroupId` | ✓ | 対応する収集の GroupId |
| `Label` | | 既定 `bench` |
| `Score` | | スコア。回によらず「大きいほど良い」1本の数値 |
| `Passed` | | ベンチが成功したか。**失敗した走行も記録する**（どの施策で壊したかを追うため） |
| `Target` | | 走行の対象ホスト |
| `ErrorCount` | | ベンチが報告したエラー件数 |
| `StartedAt` / `FinishedAt` | | 走行区間。他の計測と突き合わせるのに使う |
| `Raw` | | ベンチの生出力。**回ごとに違う減点内訳を後から読むために丸ごと持つ** |

スコアの見方は回ごとに違う（合計スコアだけでなく、成功/失敗リクエスト数や減点内訳が出る回もある）。
`Score` の数値1本だけに寄せず `Raw` を残しておくと、回をまたいで使い回せる。

## 取り出す

| API | 内容 |
| --- | --- |
| `GET /api/score` | 一覧。`Message` に `score=18902 pass errors=3 target=isu1 120s` の要約が入る |
| `GET /api/score/latest` | 最も新しいスコア |
| `GET /api/score/:id` | 個別のスコア |

UI では group の一覧の見出しにスコアが出る（失敗した走行は赤）。
`score` タブから走行だけを並べて見ることもできる。
