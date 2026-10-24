## pprotein-cli summary diff

2つの収集の httplog / slowlog を、増減（IMPACT）の大きい順に比べる。AFTER を省くと latest

```
pprotein-cli summary diff BEFORE [AFTER] [flags]
```

### Options

```
  -h, --help      help for diff
      --top int   各表に出す行数（0 で全部） (default 10)
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli summary](pprotein-cli_summary.md)	 - 1回の収集を要約する（httplog / slowlog / pprof / resource / memo）

