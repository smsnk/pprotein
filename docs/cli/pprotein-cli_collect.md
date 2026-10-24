## pprotein-cli collect

収集を開始し、完了まで待って summary を出す

```
pprotein-cli collect [flags]
```

### Options

```
  -h, --help      help for collect
      --no-wait   収集を開始するだけで、完了を待たない
      --top int   各表に出す行数（0 で全部） (default 10)
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli](pprotein-cli.md)	 - pprotein の計測結果をテキストで取り出す

