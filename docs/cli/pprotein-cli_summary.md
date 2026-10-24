## pprotein-cli summary

1回の収集を要約する（httplog / slowlog / pprof / resource / memo）

```
pprotein-cli summary [GROUP] [flags]
```

### Options

```
  -h, --help      help for summary
      --top int   各表に出す行数（0 で全部） (default 10)
      --wait      その収集のうち summary が読むエントリが確定するまで待ってから出す（上限は --timeout）
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli](pprotein-cli.md)	 - pprotein の計測結果をテキストで取り出す
* [pprotein-cli summary diff](pprotein-cli_summary_diff.md)	 - 2つの収集の httplog / slowlog を、増減（IMPACT）の大きい順に比べる。AFTER を省くと latest

