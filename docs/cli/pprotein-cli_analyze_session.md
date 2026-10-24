## pprotein-cli analyze session

1セッションの要求の列（SESSION_ID は sessions の ID 列、s001 など）

```
pprotein-cli analyze session GROUP SESSION_ID [flags]
```

### Options

```
  -h, --help   help for session
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli analyze](pprotein-cli_analyze.md)	 - 1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）

