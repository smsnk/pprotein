<template>
  <section>
    <div class="controls">
      <label>
        表示する収集数
        <select v-model.number="limit" @change="load">
          <option v-for="n in [10, 20, 30, 50]" :key="n" :value="n">{{ n }}</option>
        </select>
      </label>
      <button @click="load">Reload</button>
      <span v-if="loading">読み込み中 ...</span>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-else-if="!groups.length && !loading">収集がありません。</p>

    <template v-if="groups.length">
      <!-- ホバー中の収集がどの施策かを出す -->
      <div class="detail">
        <template v-if="current">
          <strong>{{ current.ID }}</strong>
          <span v-if="current.Datetime"> / {{ current.Datetime }}</span>
          <span v-if="current.Score" :class="current.Score.Passed ? 'pass' : 'fail'">
            / score {{ current.Score.Value.toLocaleString() }}
            {{ current.Score.Passed ? "" : "(FAIL)" }}
          </span>
          <div v-if="current.Commit" class="commit">
            {{ current.Commit.Hash.substring(0, 7) }}
            {{ firstLine(current.Commit.Message) }}
          </div>
        </template>
        <span v-else>グラフにカーソルを合わせると、その収集のコミットとスコアが出ます。</span>
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

      <h3>エンドポイント別の推移</h3>
      <label>
        <select v-model="endpointKey">
          <option value="">（選択してください）</option>
          <option v-for="k in endpointKeys" :key="k" :value="k">{{ k }}</option>
        </select>
      </label>
      <TrendChart
        v-if="endpointKey"
        :title="endpointKey"
        unit="s"
        :points="points"
        :series="endpointSeries"
        :hovered="hovered"
        @hover="hovered = $event"
      />

      <h3>直近2回の差分（IMPACT の大きい順）</h3>
      <p class="note">
        IMPACT = 1件あたりの増減 × 件数。固定時間のベンチでは速くしても
        SUM がほとんど動かない（代わりに件数が増える）ため、SUM では並べない。
      </p>
      <table v-if="diffRows.length">
        <thead>
          <tr>
            <th>ENDPOINT</th>
            <th>COUNT</th>
            <th>AVG</th>
            <th>SUM</th>
            <th>IMPACT</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in diffRows" :key="r.key">
            <td>{{ r.key }}</td>
            <td class="n">{{ fmt(r.countBefore) }} → {{ fmt(r.countAfter) }}</td>
            <td class="n">{{ fmt(r.avgBefore) }} → {{ fmt(r.avgAfter) }}</td>
            <td class="n">{{ fmt(r.sumBefore) }} → {{ fmt(r.sumAfter) }}</td>
            <td :class="['n', r.impact > 0 ? 'worse' : 'better']">
              {{ signed(r.impact) }}
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else>比較できる収集が2件以上必要です。</p>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TrendChart, { Series } from "./TrendChart.vue";

interface TrendSeries {
  Label: string;
  Key: string;
  Count: number;
  Sum: number;
  Avg: number;
}

interface Point {
  ID: string;
  Datetime: string;
  Commit?: { Hash: string; Message: string; Ref: string };
  Score?: { Value: number; Passed: boolean };
  Metrics: { [key: string]: number };
  Endpoints: TrendSeries[] | null;
  Queries: TrendSeries[] | null;
}

export default defineComponent({
  components: { TrendChart },
  data() {
    return {
      groups: [] as Point[],
      limit: 20,
      hovered: -1,
      endpointKey: "",
      loading: false,
      error: "",
    };
  },
  computed: {
    current(): Point | null {
      return this.hovered >= 0 ? this.groups[this.hovered] ?? null : null;
    },
    points() {
      return this.groups.map((g) => ({
        label: g.Commit ? g.Commit.Hash.substring(0, 7) : g.ID.substring(11, 19),
      }));
    },
    charts() {
      const metric = (key: string, name: string): Series => ({
        name,
        values: this.groups.map((g) =>
          key in g.Metrics ? g.Metrics[key] : null,
        ),
      });
      return [
        { title: "score", unit: "", series: [metric("score", "score")] },
        {
          title: "httplog: 合計レスポンスタイム",
          unit: "s",
          series: [metric("httplog.sum", "sum")],
        },
        {
          title: "httplog: リクエスト総数",
          unit: "",
          series: [metric("httplog.count", "count")],
        },
        {
          title: "slowlog: 合計クエリ時間",
          unit: "s",
          series: [metric("slowlog.sum", "sum")],
        },
        {
          title: "slowlog: 走査行数の合計",
          unit: "",
          series: [metric("slowlog.rows_examined", "rows examined")],
        },
      ];
    },
    endpointKeys(): string[] {
      const keys = new Set<string>();
      this.groups.forEach((g) =>
        (g.Endpoints || []).forEach((e) => keys.add(`${e.Label} ${e.Key}`)),
      );
      return [...keys].sort();
    },
    endpointSeries(): Series[] {
      const find = (g: Point) =>
        (g.Endpoints || []).find(
          (e) => `${e.Label} ${e.Key}` === this.endpointKey,
        );
      return [
        { name: "sum", values: this.groups.map((g) => find(g)?.Sum ?? null) },
        { name: "avg", values: this.groups.map((g) => find(g)?.Avg ?? null) },
      ];
    },
    diffRows() {
      if (this.groups.length < 2) {
        return [];
      }
      const before = this.groups[this.groups.length - 2];
      const after = this.groups[this.groups.length - 1];
      const key = (e: TrendSeries) => `${e.Label} ${e.Key}`;

      const bmap = new Map((before.Endpoints || []).map((e) => [key(e), e]));
      const amap = new Map((after.Endpoints || []).map((e) => [key(e), e]));

      const rows = [...new Set([...bmap.keys(), ...amap.keys()])].map((k) => {
        const b = bmap.get(k);
        const a = amap.get(k);
        const countBefore = b?.Count ?? 0;
        const countAfter = a?.Count ?? 0;
        const avgBefore = b?.Avg ?? 0;
        const avgAfter = a?.Avg ?? 0;
        const n = (countBefore + countAfter) / 2;
        return {
          key: k,
          countBefore,
          countAfter,
          avgBefore,
          avgAfter,
          sumBefore: b?.Sum ?? 0,
          sumAfter: a?.Sum ?? 0,
          impact: n === 0 ? 0 : (avgAfter - avgBefore) * n,
        };
      });
      return rows
        .sort((x, y) => Math.abs(y.impact) - Math.abs(x.impact))
        .slice(0, 20);
    },
  },
  async created() {
    await this.load();
  },
  methods: {
    async load() {
      this.loading = true;
      this.error = "";
      try {
        const resp = await fetch(`/api/trend?limit=${this.limit}`);
        if (!resp.ok) {
          this.error = `http error: status=${resp.status}`;
          return;
        }
        const data = await resp.json();
        this.groups = data.Groups || [];
      } catch (e) {
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
    firstLine(message: string) {
      return message.split("\n")[0];
    },
    fmt(v: number) {
      return v >= 1000 ? v.toFixed(0) : v.toFixed(3);
    },
    signed(v: number) {
      const s = Math.abs(v) >= 1000 ? Math.abs(v).toFixed(0) : Math.abs(v).toFixed(3);
      if (v > 0) return `+${s}`;
      if (v < 0) return `-${s}`;
      return "0";
    },
  },
});
</script>

<style scoped lang="scss">
section {
  padding: 2em;
  overflow-y: auto;
}

.controls {
  display: flex;
  align-items: center;
  gap: 1em;
  margin-bottom: 1em;
}

.detail {
  margin-bottom: 1em;
  padding: 0.6em 1em;
  background: #f0f0f0;
  border-left: 0.3em solid #999;
  min-height: 3em;

  .commit {
    margin-top: 0.3em;
    color: #555;
  }

  .pass {
    color: #2c6fbb;
  }

  .fail {
    color: #c0392b;
  }
}

h3 {
  margin-top: 2em;
}

.note {
  margin: 0.3em 0 0 0;
  color: #666;
  font-size: 0.9em;
}

table {
  border-collapse: collapse;
  margin-top: 1em;
}

th,
td {
  padding: 0.4em 1em;
  border: 1px solid #999;
  text-align: left;
}

.n {
  text-align: right;
}

.better {
  color: #2c6fbb;
}

.worse {
  color: #c0392b;
}

.error {
  color: #c0392b;
}
</style>
