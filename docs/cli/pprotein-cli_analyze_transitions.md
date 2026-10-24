## pprotein-cli analyze transitions

よくある遷移（ある要求の次に叩くエンドポイント）

```
pprotein-cli analyze transitions [GROUP] [flags]
```

### Options

```
  -h, --help               help for transitions
      --lifecycle string   そのライフサイクルに入ったセッションに絞る
      --limit int          出す遷移の数（0 で全部） (default 30)
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli analyze](pprotein-cli_analyze.md)	 - 1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）

