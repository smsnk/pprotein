## pprotein-cli

pprotein の計測結果をテキストで取り出す

### Synopsis

pprotein の計測結果を、エージェントの文脈にそのまま貼れる Markdown（--json なら JSON）で出す。

系統は2つある。
  summary  サーバーの中で何が遅いか（alp / slp / pprof / resource の集計）
  analyze  得点がいつどこで生まれ、利用者がどこまで進んだか（httplog の生ログと dbquery）

GROUP は収集の GroupId（2026-10-17_10-41-02.984213 の形）。省略すると latest で、
収集中のものも含め、全部が失敗した収集を除いた最新の収集を指す（飛ばした収集は stderr に書く）。

### Options

```
  -h, --help               help for pprotein-cli
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli analyze](pprotein-cli_analyze.md)	 - 1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）
* [pprotein-cli collect](pprotein-cli_collect.md)	 - 収集を開始し、完了まで待って summary を出す
* [pprotein-cli groups](pprotein-cli_groups.md)	 - 収集の一覧（新しい順。コミット・スコア付き）
* [pprotein-cli summary](pprotein-cli_summary.md)	 - 1回の収集を要約する（httplog / slowlog / pprof / resource / memo）

