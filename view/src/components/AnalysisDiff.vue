<template>
  <section>
    <div class="controls">
      <router-link to="/analysis/">← 走行の推移</router-link>
      <button @click="load">Reload</button>
      <span v-if="loading">読み込み中 ...</span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="d">
      <h2>比較</h2>
      <div class="detail">
        <div>
          before:
          <router-link :to="`/analysis/${d.Before}/`">{{
            d.Before
          }}</router-link>
          <span v-if="d.Score?.Before">
            / score {{ d.Score.Before.Value.toLocaleString() }}</span
          >
        </div>
        <div>
          after:
          <router-link :to="`/analysis/${d.After}/`">{{ d.After }}</router-link>
          <span v-if="d.Score?.After">
            / score {{ d.Score.After.Value.toLocaleString() }}</span
          >
          <strong v-if="scoreChange" :class="scoreChange.cls">
            {{ scoreChange.text }}</strong
          >
        </div>
      </div>
      <p class="note">
        2走行の比較なので有意差は判定しません。同じコードの2走行でも、件数の少ない系列やライフサイクルの後段の所要時間は
        ±25% ほど動きます。判断には score
        の系列の合計・リクエスト総数・エンドポイント構成比を使ってください。
      </p>
      <p v-for="w in d.Warnings || []" :key="w" class="warn">⚠ {{ w }}</p>

      <h3>系列（両方にあるものは変化率の大きい順）</h3>
      <table>
        <thead>
          <tr>
            <th>系列</th>
            <th>役割</th>
            <th>before</th>
            <th>after</th>
            <th>差</th>
            <th>50% 到達</th>
            <th>90% 到達</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in d.Series || []" :key="s.Name">
            <td>{{ s.Name }}</td>
            <td>{{ s.Role || "" }}</td>
            <td class="n">{{ num(s.Before) }}</td>
            <td class="n">{{ num(s.After) }}</td>
            <td class="n" :class="seriesChange(s).cls">
              {{ seriesChange(s).text }}
            </td>
            <td class="n">{{ secPair(s.T50SecBefore, s.T50SecAfter) }}</td>
            <td class="n">{{ secPair(s.T90SecBefore, s.T90SecAfter) }}</td>
          </tr>
        </tbody>
      </table>

      <div class="grid2">
        <div>
          <h3>リクエスト</h3>
          <table>
            <thead>
              <tr>
                <th>指標</th>
                <th>before</th>
                <th>after</th>
                <th>差</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in d.Requests" :key="m.Name">
                <td>{{ m.Name }}</td>
                <td class="n">
                  {{ isTime(m.Name) ? sec(m.Before) : num(m.Before) }}
                </td>
                <td class="n">
                  {{ isTime(m.Name) ? sec(m.After) : num(m.After) }}
                </td>
                <td class="n" :class="metricChange(m).cls">
                  {{ metricChange(m).text }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div>
          <h3>エンドポイント構成比（比率の差が大きい順）</h3>
          <table>
            <thead>
              <tr>
                <th>エンドポイント</th>
                <th>件数</th>
                <th>比率</th>
                <th>差</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in d.Endpoints || []" :key="e.Key">
                <td class="key">{{ e.Key }}</td>
                <td class="n">{{ e.CountBefore }} → {{ e.CountAfter }}</td>
                <td class="n">
                  {{ pct(e.ShareBefore) }} → {{ pct(e.ShareAfter) }}
                </td>
                <td class="n">{{ pt(e.ShareAfter - e.ShareBefore) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <template v-for="l in d.Lifecycles || []" :key="l.Name">
        <h3>
          ライフサイクル: {{ l.Name }}（エンティティ
          {{ l.EntitiesBefore ?? "-" }} → {{ l.EntitiesAfter ?? "-" }}）
        </h3>
        <Funnel
          :steps="l.Steps.map((s) => s.Before)"
          :after="l.Steps.map((s) => s.After)"
          :names="l.Steps.map((s) => s.Name)"
        />
      </template>

      <template v-for="b in d.Breakdowns || []" :key="b.Name">
        <h3>
          内訳: {{ b.Name }}（行数 {{ b.RowsBefore ?? "-" }} →
          {{ b.RowsAfter ?? "-" }}）
        </h3>
        <p v-if="!b.Keyed" class="note">
          key / value 列が無いので行数だけ比べます。
        </p>
        <template v-else>
          <p class="note">
            上位1件の構成比 {{ pct(b.Top1Before) }} →
            {{ pct(b.Top1After) }}、上位3件 {{ pct(b.Top3Before) }} →
            {{ pct(b.Top3After) }}
            <span v-if="b.NewInTop?.length"
              >／ 新たに上位: {{ b.NewInTop.join(", ") }}</span
            >
          </p>
          <table>
            <thead>
              <tr>
                <th>key</th>
                <th>before</th>
                <th>after</th>
                <th>差</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in b.Rows || []" :key="r.Key">
                <td>{{ r.Key }}</td>
                <td class="n">
                  {{ r.Before === undefined ? "（上位外）" : num(r.Before) }}
                </td>
                <td class="n">
                  {{ r.After === undefined ? "（上位外）" : num(r.After) }}
                </td>
                <td class="n">
                  {{
                    r.Before !== undefined && r.After !== undefined
                      ? change(r.Before, r.After).text
                      : ""
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </template>
      </template>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import Funnel from "./Funnel.vue";
import { Diff, change, getJSON, num, pct, sec } from "../analysis";

// エラー・遅延の指標は減るほど良い
const LOWER_IS_BETTER = new Set(["4xx", "5xx", "499", "p50", "p99"]);

export default defineComponent({
  components: { Funnel },
  data() {
    return {
      d: null as Diff | null,
      loading: false,
      error: "",
    };
  },
  computed: {
    a(): string {
      return this.$route.params.a as string;
    },
    b(): string {
      return this.$route.params.b as string;
    },
    scoreChange() {
      const s = this.d?.Score;
      return s?.Before && s?.After
        ? change(s.Before.Value, s.After.Value)
        : null;
    },
  },
  watch: {
    a() {
      this.load();
    },
    b() {
      this.load();
    },
  },
  async created() {
    await this.load();
  },
  methods: {
    num,
    pct,
    sec,
    change,
    async load() {
      this.loading = true;
      this.error = "";
      try {
        const q = new URLSearchParams({
          before: this.a,
          after: this.b,
          top: "15",
        });
        this.d = await getJSON<Diff>(`/api/analysis/diff?${q}`);
      } catch (e) {
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
    // penalty の系列は減るほど良い。役割の無い系列は色を付けない。
    seriesChange(s: { Role?: string; Before?: number; After?: number }) {
      const c = change(s.Before, s.After, s.Role !== "penalty");
      return s.Role ? c : { ...c, cls: "" };
    },
    metricChange(m: { Name: string; Before: number; After: number }) {
      return change(m.Before, m.After, !LOWER_IS_BETTER.has(m.Name));
    },
    isTime(name: string) {
      return name === "p50" || name === "p99";
    },
    secPair(b?: number, a?: number) {
      const f = (v?: number) => (v === undefined ? "-" : `${v}s`);
      return `${f(b)} → ${f(a)}`;
    },
    pt(v: number) {
      return `${v >= 0 ? "+" : ""}${(v * 100).toFixed(1)}pt`;
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

h2 {
  margin: 0.2em 0 0.5em 0;
}

.key {
  word-break: break-all;
}

tbody tr {
  cursor: default;
}
</style>
