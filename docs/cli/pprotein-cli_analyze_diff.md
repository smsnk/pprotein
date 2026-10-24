## pprotein-cli analyze diff

2つの走行の analyze を比べる（系列・リクエスト・エンドポイント構成比・ライフサイクル・内訳）。AFTER を省くと latest

```
pprotein-cli analyze diff BEFORE [AFTER] [flags]
```

### Options

```
  -h, --help      help for diff
      --top int   エンドポイント構成比の表に出す行数（0 で全部） (default 10)
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli analyze](pprotein-cli_analyze.md)	 - 1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）

