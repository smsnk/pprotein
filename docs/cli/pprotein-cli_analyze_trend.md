## pprotein-cli analyze trend

直近の走行を1行ずつ並べる（得点・母数・利用者の進み方の推移）

```
pprotein-cli analyze trend [flags]
```

### Options

```
  -h, --help        help for trend
      --limit int   遡る走行の数（1〜100） (default 20)
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli analyze](pprotein-cli_analyze.md)	 - 1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）

