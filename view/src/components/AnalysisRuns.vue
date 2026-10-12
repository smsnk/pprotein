<template>
  <section>
    <div class="controls">
      <label>
        表示する走行数
        <select v-model.number="limit" @change="load">
          <option v-for="n in [10, 20, 30, 50]" :key="n" :value="n">
            {{ n }}
          </option>
        </select>
      </label>
      <button @click="load">Reload</button>
      <span v-if="loading">読み込み中 ...</span>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="!runs.length && !loading">
      走行がありません（httplog の収集と analysis.yml の設定が要ります）。
    </p>

    <template v-if="runs.length">
      <p v-if="configChanged" class="warn">
        走行によって analysis.yml / dbquery.yml
        が違います。系列の値は、その境目の前後で比べられないことがあります。
      </p>

      <div class="detail">
        <template v-if="current">
          <strong>{{ current.GroupId }}</strong>
          <span
            v-if="current.Score"
            :class="current.Score.Passed ? 'pass' : 'fail'"
          >
            / score {{ current.Score.Value.toLocaleString()
            }}{{ current.Score.Passed ? "" : " (FAIL)" }}
          </span>
          <div v-if="current.Commit" class="commit">
            {{ shortHash(current.Commit) }} {{ current.Commit.Message }}
          </div>
          <div v-if="current.Warnings" class="warn-inline">
            ⚠ {{ current.Warnings.join(" / ") }}
          </div>
        </template>
        <span v-else
          >グラフにカーソルを合わせると、その走行のコミットとスコアが出ます。</span
        >
      </div>

      <TrendChart
        v-for="chart in charts"
        :key="chart.title"
        :title="chart.title"
        :unit="chart.unit"
        :points="points"
        :series="chart.series"
        :hovered="hovered"
        @hover="hovered = $event"
      />

      <h3>リクエストの内訳（エンドポイント別）</h3>
      <p class="note">
        棒の高さがリクエスト総数、区分の厚みがエンドポイントごとの件数です。チェックしたエンドポイントを色分けし、
        残りは「その他」にまとめます（既定は直近の走行で件数の上位5件）。下の表はカーソルを合わせた走行の内訳です。
      </p>
      <StackedChart
        title="リクエスト件数（エンドポイント別の積み上げ）"
        :points="points"
        :series="endpointStack"
        :hovered="hovered"
        @hover="hovered = $event"
      />
      <div class="grid2">
        <TrendChart
          v-for="chart in endpointCharts"
          :key="chart.title"
          :title="chart.title"
          :unit="chart.unit"
          :points="points"
          :series="chart.series"
          :hovered="hovered"
          @hover="hovered = $event"
        />
      </div>
      <div class="ep-picker">
        <label v-for="k in endpointKeys" :key="k">
          <input v-model="endpoints" type="checkbox" :value="k" />
          {{ prettyEndpoint(k) }}
        </label>
      </div>

      <h3>走行の一覧</h3>
      <p class="note">
        行をクリックするとその走行の詳細へ。2つにチェックを入れると比較できます（古い方が
        before）。
      </p>
      <div class="controls">
        <button :disabled="selected.length !== 2" @click="openDiff">
          選んだ2走行を比較
        </button>
        <span v-if="selected.length">{{ selected.length }} 件選択中</span>
      </div>
      <table>
        <thead>
          <tr>
            <th />
            <th>GroupId</th>
            <th>commit</th>
            <th>score</th>
            <th v-for="s in roleSeries" :key="s.name">{{ s.name }}</th>
            <th>req</th>
            <th>499</th>
            <th>p99</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(r, i) in runs"
            :key="r.GroupId"
            :class="{ hovered: i === hovered }"
            @click="open(r.GroupId)"
            @mouseenter="hovered = i"
          >
            <td @click.stop>
              <input v-model="selected" type="checkbox" :value="r.GroupId" />
            </td>
            <td>
              {{ r.GroupId
              }}<span v-if="r.Warnings" title="集計が欠けている"> ⚠</span>
            </td>
            <td>{{ shortHash(r.Commit) }}</td>
            <td class="n" :class="r.Score && !r.Score.Passed ? 'fail' : ''">
              {{ r.Score ? r.Score.Value.toLocaleString() : "-" }}
            </td>
            <td v-for="s in roleSeries" :key="s.name" class="n">
              {{ num(total(r, s.name)) }}
            </td>
            <td class="n">{{ r.Requests.Requests.toLocaleString() }}</td>
            <td class="n">{{ r.Requests.S499 }}</td>
            <td class="n">{{ sec(r.Requests.P99Sec) }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TrendChart, { Series } from "./TrendChart.vue";
import StackedChart, { OTHER, StackSeries } from "./StackedChart.vue";
import {
  ROLES,
  ROLE_LABEL,
  RunSummary,
  getJSON,
  num,
  sec,
  shortHash,
  EndpointStat,
  prettyEndpoint,
} from "../analysis";

export default defineComponent({
  components: { TrendChart, StackedChart },
  data() {
    return {
      runs: [] as RunSummary[],
      limit: 20,
      hovered: -1,
      selected: [] as string[],
      endpoints: [] as string[],
      loading: false,
      error: "",
    };
  },
  computed: {
    current(): RunSummary | null {
      return this.hovered >= 0 ? this.runs[this.hovered] ?? null : null;
    },
    points() {
      return this.runs.map((r) => ({
        label: r.Commit ? shortHash(r.Commit) : r.GroupId.substring(11, 19),
      }));
    },
    configChanged(): boolean {
      return (
        new Set(
          this.runs.map(
            (r) => `${r.ConfigHash.Analysis}/${r.ConfigHash.DBQuery}`,
          ),
        ).size > 1
      );
    },
    // 役割の付いた系列（どの走行かに出てくるもの）
    roleSeries(): { name: string; role: string }[] {
      const seen = new Map<string, string>();
      this.runs.forEach((r) =>
        (r.Series || []).forEach((s) => {
          if (s.Role && !seen.has(s.Name)) seen.set(s.Name, s.Role);
        }),
      );
      return [...seen.entries()]
        .map(([name, role]) => ({ name, role }))
        .sort(
          (a, b) =>
            ROLES.indexOf(a.role as never) - ROLES.indexOf(b.role as never),
        );
    },
    // 直近の走行で件数の多い順、そのあとに過去の走行にしか無いもの
    endpointKeys(): string[] {
      const keys: string[] = [];
      [...this.runs].reverse().forEach((r) =>
        (r.Endpoints || []).forEach((e) => {
          if (!keys.includes(e.Key)) keys.push(e.Key);
        }),
      );
      return keys;
    },
    // 選んだエンドポイントの件数 + その他（総数との差）。棒の高さが総数と一致する
    endpointStack(): StackSeries[] {
      const count = (r: RunSummary, key: string) =>
        (r.Endpoints || []).find((e) => e.Key === key)?.Count ?? 0;
      const picked = this.endpoints.map((k) => ({
        name: prettyEndpoint(k),
        values: this.runs.map((r) => count(r, k)),
      }));
      const other = this.runs.map(
        (r, i) =>
          r.Requests.Requests - picked.reduce((sum, s) => sum + s.values[i], 0),
      );
      return [...picked, { name: OTHER, values: other }];
    },
    endpointCharts() {
      const of = (r: RunSummary, key: string) =>
        (r.Endpoints || []).find((e) => e.Key === key);
      const series = (f: (e: EndpointStat) => number): Series[] =>
        this.endpoints.map((k) => ({
          name: prettyEndpoint(k),
          values: this.runs.map((r) => {
            const e = of(r, k);
            return e ? f(e) : null;
          }),
        }));
      // 件数と構成比は積み上げの棒で見る。単位の違う p99 とエラーは折れ線で
      return [
        { title: "p99", unit: "s", series: series((e) => e.P99Sec) },
        {
          title: "エラー + 499",
          unit: "",
          series: series((e) => e.Errors + e.S499),
        },
      ];
    },
    charts() {
      const charts: { title: string; unit: string; series: Series[] }[] = [];
      const seriesOf = (name: string): Series => ({
        name,
        values: this.runs.map((r) => this.total(r, name) ?? null),
      });

      charts.push({
        title: "スコア",
        unit: "",
        series: [
          {
            name: "score",
            values: this.runs.map((r) => r.Score?.Value ?? null),
          },
        ],
      });
      for (const role of ROLES) {
        const names = this.roleSeries
          .filter((s) => s.role === role)
          .map((s) => s.name);
        if (names.length) {
          charts.push({
            title: `系列: ${ROLE_LABEL[role]}`,
            unit: "",
            series: names.map(seriesOf),
          });
        }
      }

      const lifecycles = new Set<string>();
      this.runs.forEach((r) =>
        (r.Lifecycles || []).forEach((l) => lifecycles.add(l.Name)),
      );
      for (const name of lifecycles) {
        const find = (r: RunSummary) =>
          (r.Lifecycles || []).find((l) => l.Name === name);
        const last = this.runs.map((r) => {
          const l = find(r);
          return l && l.Steps.length ? l.Steps[l.Steps.length - 1] : undefined;
        });
        const lastName = last.find((s) => s)?.Name ?? "最後";
        charts.push({
          title: `ライフサイクル: ${name}`,
          unit: "",
          series: [
            {
              name: "開始",
              values: this.runs.map((r) => find(r)?.Entities ?? null),
            },
            {
              name: `${lastName} まで到達`,
              values: last.map((s) => s?.Reached ?? null),
            },
          ],
        });
      }

      // リクエスト総数はエンドポイント別の積み上げの棒の高さで見る
      charts.push({
        title: "エラー",
        unit: "",
        series: [
          { name: "499", values: this.runs.map((r) => r.Requests.S499) },
          { name: "5xx", values: this.runs.map((r) => r.Requests.S5xx) },
          { name: "4xx", values: this.runs.map((r) => r.Requests.S4xx) },
        ],
      });
      charts.push({
        title: "応答時間",
        unit: "s",
        series: [
          { name: "p50", values: this.runs.map((r) => r.Requests.P50Sec) },
          { name: "p99", values: this.runs.map((r) => r.Requests.P99Sec) },
        ],
      });
      return charts;
    },
  },
  async created() {
    await this.load();
  },
  methods: {
    prettyEndpoint,
    num,
    sec,
    shortHash,
    async load() {
      this.loading = true;
      this.error = "";
      try {
        const data = await getJSON<{ Runs: RunSummary[] }>(
          `/api/analysis?limit=${this.limit}&endpoints=1`,
        );
        this.runs = data.Runs || [];
        if (!this.endpoints.length) {
          this.endpoints = this.endpointKeys.slice(0, 5);
        }
      } catch (e) {
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
    total(r: RunSummary, name: string): number | undefined {
      return (r.Series || []).find((s) => s.Name === name)?.Total;
    },
    open(gid: string) {
      this.$router.push(`/analysis/${gid}/`);
    },
    openDiff() {
      const [a, b] = [...this.selected].sort();
      this.$router.push(`/analysis/diff/${a}/${b}/`);
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

.ep-picker {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 0.2em 1em;
  margin-bottom: 1em;
  font-size: 0.9em;

  label {
    cursor: pointer;
  }
}
</style>
