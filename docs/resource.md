# 走行中のリソース使用状況

alp / slow query log / pprof は「アプリと DB の中で何が遅いか」は教えてくれるが、
**どのプロセスがどのホストの CPU / メモリを食い切っているか**は分からない。

- アプリのプロファイルが平坦でも、ホストの CPU は MySQL 側で張り付いていることがある
- 何台目のサーバーに何を寄せるか（DB 分離、アプリの複数台化）はこの情報が無いと決められない

`resource` は走行中に各ホストの `/proc` を一定間隔で読み、他の計測と**同じ収集**として保存する。
走行区間との対応が残るので、後追いで「あの走行で何が CPU を食っていたか」を確認できる。

## 使う

`targets.json` に追加する（`Duration` は他の計測と同じくベンチの走行時間に合わせる）。

```json
{
  "Type": "resource",
  "Label": "isu1",
  "URL": "http://localhost:9010/debug/resource",
  "Duration": 90
}
```

`/debug/resource` は `integration.RegisterDebugHandlers` が生やすので、
**アプリに integration を組み込んでいれば何もしなくても生える**。
`pprotein-agent` を各ホストで動かしている場合はそちらでも配信される。

サンプリング間隔の既定は 5 秒。`PPROTEIN_RESOURCE_INTERVAL`（秒）で変えられる。
**常駐して走行中に動くものなので、間隔は粗いままにしておく。**
最終走行の前は、他の計測エージェントと同じく止めること。

## 出るもの

```
host: isu1 (4 cpu)
window: 10:41:02 - 10:42:32 (19 samples)

[host cpu %]
              avg     peak
busy         92.4     99.1
user         61.0     70.2
system       18.4     22.0
iowait       12.1     30.5
steal         0.9      1.2
irq           0.0      0.1

[load1] avg=7.82 peak=11.30
[mem] total=3.8G used avg=2.9G peak=3.1G (81.4%)

[disk]
device        read KB/s   write KB/s    util%
vda                12.4       8420.1     71.2

[top processes] cpu% はコア1本を100%とした値
cpu% avg     peak    rss avg   rss peak  command
   210.4    280.1       1.8G       1.9G  mysqld (1234)
    88.2    120.5     420.0M     460.1M  isuride (2345)
     6.1      9.0      12.0M      13.2M  nginx (999)
```

- **プロセスの cpu% はコア1本を 100% とした値。** 4コアなら合計 400% まで出る
- `iowait` が高ければディスク、`steal` が高ければホストの取り合い、`system` が高ければ
  システムコール（コネクション張りすぎなど）を疑う
- 生のサンプル列は `/api/resource/data/:id` からダウンロードできる

## 実装メモ

- 取得は `/proc` の直読み。`top` や `pidstat` に依存しない
- プロセスの CPU は **「全 CPU の jiffies 差分に対する比 × コア数」** で出している。
  `USER_HZ` を仮定せずに済む
- pid が使い回された場合（コマンド名が変わった場合）は集計から外す
- 累積カウンタが巻き戻ったときは 0 として扱う
- サンプルが1件しか取れなかった場合は差分が出せないので、その旨を出力する
  （`Duration` を interval より長くする）
