# pprotein

[kaz/pprotein](https://github.com/kaz/pprotein) のフォーク。ISUCON 向けのプロファイリングツールに、
ベンチのスコア・走行中のリソース使用量・走行の分析と、AI エージェント向けの CLI を足している。

```sh
make build   # pprotein（UI 込み）・pprotein-agent・pprotein-cli
```

## ドキュメント

| | |
| --- | --- |
| [docs/cli.md](docs/cli.md) | pprotein-cli の使い方（コマンドのリファレンスは [docs/cli/](docs/cli/pprotein-cli.md)） |
| [docs/analysis.md](docs/analysis.md) | 走行の得点内訳と利用者の行動（dbquery / analysis） |
| [docs/score.md](docs/score.md) | ベンチのスコアを収集に紐づける |
| [docs/resource.md](docs/resource.md) | 走行中のリソース使用状況 |
| [docs/trend.md](docs/trend.md) | 収集をまたいだ推移（UI） |
