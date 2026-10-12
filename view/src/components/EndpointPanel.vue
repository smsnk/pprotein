<template>
  <div>
    <h3>リクエストの内訳（エンドポイント別、全 {{ stats.length }} 件）</h3>
    <p class="note">
      棒の高さが区間ごとのリクエスト総数、区分の厚みがエンドポイントごとの件数です。チェックしたエンドポイントを色分けし、
      残りは「その他」にまとめます（既定は件数の上位5件）。下の表の行をクリックすると選択を切り替えます。
    </p>
    <StackedChart
      :x-label="`収集開始からの秒（${tl?.BucketSec ?? 5}秒区間）`"
      title="リクエスト件数（区間ごと、エンドポイント別の積み上げ）"
      :points="points"
      :series="stack"
      :hovered="hovered"
      @hover="hovered = $event"
    />
    <div class="grid2">
      <TrendChart
        :x-label="`収集開始からの秒（${tl?.BucketSec ?? 5}秒区間）`"
        v-for="chart in charts"
        :key="chart.title"
        :title="chart.title"
        :unit="chart.unit"
        :points="points"
        :series="chart.series"
        :hovered="hovered"
        @hover="hovered = $event"
      />
    </div>
    <table>
      <thead>
        <tr>
          <th />
          <th>エンドポイント</th>
          <th>件数</th>
          <th>構成比</th>
          <th>p50</th>
          <th>p99</th>
          <th>合計</th>
          <th>エラー</th>
          <th>499</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in stats" :key="s.Key" @click="toggle(s.Key)">
          <td>
            <input
              type="checkbox"
              :checked="selected.includes(s.Key)"
              @click.stop="toggle(s.Key)"
            />
          </td>
          <td class="ep">{{ prettyEndpoint(s.Key) }}</td>
          <td class="n">{{ s.Count.toLocaleString() }}</td>
          <td class="share-cell">
            <span class="share" :style="{ width: `${s.Share * 120}px` }" />{{
              pct(s.Share)
            }}
          </td>
          <td class="n">{{ sec(s.P50Sec) }}</td>
          <td class="n" :class="{ worse: s.P99Sec >= 1 }">
            {{ sec(s.P99Sec) }}
          </td>
          <td class="n">{{ sec(s.SumSec) }}</td>
          <td class="n" :class="{ worse: s.Errors > 0 }">{{ s.Errors }}</td>
          <td class="n" :class="{ worse: s.S499 > 0 }">{{ s.S499 }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import TrendChart, { Series } from "./TrendChart.vue";
import StackedChart, { OTHER, StackSeries } from "./StackedChart.vue";
import {
  EndpointStat,
  EndpointTimeline,
  getJSON,
  pct,
  prettyEndpoint,
  sec,
} from "../analysis";

// 1走行のエンドポイントを選んで、区間ごとの推移をグラフにする。
export default defineComponent({
  components: { TrendChart, StackedChart },
  props: {
    gid: { type: String, required: true },
    stats: { type: Array as PropType<EndpointStat[]>, required: true },
  },
  data() {
    return {
      tl: null as EndpointTimeline | null,
      selected: this.stats.slice(0, 5).map((s) => s.Key),
      hovered: -1,
    };
  },
  computed: {
    points() {
      return (this.tl?.OffsetSec || []).map((o) => ({ label: `${o}s` }));
    },
    // 選んだエンドポイントの件数 + その他（全エンドポイントの合計との差）
    stack(): StackSeries[] {
      const eps = this.tl?.Endpoints || [];
      const n = this.tl?.OffsetSec.length ?? 0;
      const totals = Array.from({ length: n }, (_, i) =>
        eps.reduce((sum, e) => sum + e.Count[i], 0),
      );
      const picked = this.selected
        .map((k) => eps.find((e) => e.Key === k))
        .filter((e): e is NonNullable<typeof e> => !!e)
        .map((e) => ({ name: prettyEndpoint(e.Key), values: e.Count }));
      const other = totals.map(
        (t, i) => t - picked.reduce((sum, s) => sum + s.values[i], 0),
      );
      return [...picked, { name: OTHER, values: other }];
    },
    charts() {
      const pick = (field: "Count" | "P99Sec" | "Errors" | "S499"): Series[] =>
        this.selected
          .map((k) => this.tl?.Endpoints.find((e) => e.Key === k))
          .filter((e): e is NonNullable<typeof e> => !!e)
          .map((e) => ({ name: prettyEndpoint(e.Key), values: e[field] }));
      // 件数は積み上げの棒で見る。単位の違う p99 とエラーは折れ線で
      return [
        { title: "p99（区間ごと）", unit: "s", series: pick("P99Sec") },
        {
          title: "エラー 4xx/5xx（区間ごと）",
          unit: "",
          series: pick("Errors"),
        },
        { title: "499（区間ごと）", unit: "", series: pick("S499") },
      ];
    },
  },
  watch: {
    gid() {
      this.load();
    },
  },
  async created() {
    await this.load();
  },
  methods: {
    pct,
    sec,
    prettyEndpoint,
    async load() {
      this.tl = await getJSON<EndpointTimeline>(
        `/api/analysis/${this.gid}/endpoints`,
      );
    },
    toggle(key: string) {
      this.selected = this.selected.includes(key)
        ? this.selected.filter((k) => k !== key)
        : [...this.selected, key];
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

.ep {
  word-break: break-all;
}

.share-cell {
  white-space: nowrap;
}
</style>
