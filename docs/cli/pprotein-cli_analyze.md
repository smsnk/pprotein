## pprotein-cli analyze

1回の走行を時系列・ライフサイクル・内訳で集計する（docs/analysis.md）

```
pprotein-cli analyze [GROUP] [flags]
```

### Options

```
  -h, --help   help for analyze
      --wait   その収集のうち analyze が読むエントリ（httplog / dbquery / score）が確定するまで待ってから出す（上限は --timeout）
```

### Options inherited from parent commands

```
      --json               JSON で出力する
      --timeout duration   API の応答と、収集の完了待ちの上限 (default 5m0s)
      --url string         pprotein の URL。環境変数 PPROTEIN_URL でも指定できる (default "http://localhost:9000")
```

### SEE ALSO

* [pprotein-cli](pprotein-cli.md)	 - pprotein の計測結果をテキストで取り出す
* [pprotein-cli analyze diff](pprotein-cli_analyze_diff.md)	 - 2つの走行の analyze を比べる（系列・リクエスト・エンドポイント構成比・ライフサイクル・内訳）。AFTER を省くと latest
* [pprotein-cli analyze endpoints](pprotein-cli_analyze_endpoints.md)	 - エンドポイント別の件数・構成比・p50・p99・合計・エラー・499（全件）
* [pprotein-cli analyze session](pprotein-cli_analyze_session.md)	 - 1セッションの要求の列（SESSION_ID は sessions の ID 列、s001 など）
* [pprotein-cli analyze sessions](pprotein-cli_analyze_sessions.md)	 - セッション（利用者）の一覧
* [pprotein-cli analyze transitions](pprotein-cli_analyze_transitions.md)	 - よくある遷移（ある要求の次に叩くエンドポイント）
* [pprotein-cli analyze trend](pprotein-cli_analyze_trend.md)	 - 直近の走行を1行ずつ並べる（得点・母数・利用者の進み方の推移）

