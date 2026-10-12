<template>
  <section>
    <div class="controls">
      <router-link to="/analysis/">← 走行の推移</router-link>
      <button @click="load">Reload</button>
      <span v-if="loading">読み込み中 ...</span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="rep">
      <h2>{{ rep.GroupId }}</h2>
      <p>
        <router-link :to="`/analysis/${gid}/sessions/`"
          >セッションの導線を見る →</router-link
        >
      </p>
      <div class="detail">
        <span v-if="rep.Score" :class="rep.Score.Passed ? 'pass' : 'fail'">
          score {{ rep.Score.Value.toLocaleString()
          }}{{ rep.Score.Passed ? "" : " (FAIL)" }}
        </span>
        <span v-else>score なし</span>
        <span> / 0s = 収集開始 {{ origin }} / 区間 {{ rep.BucketSec }}s</span>
        <div v-if="rep.Commit" class="commit">
          {{ shortHash(rep.Commit) }} {{ rep.Commit.Message }}
        </div>
        <div class="commit">
          設定: analysis.yml {{ rep.ConfigHash.Analysis || "-" }} / dbquery.yml
          {{ rep.ConfigHash.DBQuery || "-" }}
        </div>
      </div>
      <p v-for="w in rep.Warnings || []" :key="w" class="warn">⚠ {{ w }}</p>

      <div class="grid2">
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
      </div>

      <EndpointPanel
        v-if="rep.EndpointStats?.length"
        :gid="gid"
        :stats="rep.EndpointStats"
      />

      <template v-for="l in rep.Lifecycles || []" :key="l.Name">
        <h3>
          ライフサイクル: {{ l.Name }}（{{ l.Entities }} エンティティ）
          <router-link
            class="sub-link"
            :to="`/analysis/${gid}/sessions/?lifecycle=${l.Name}`"
          >
            このライフサイクルのセッション →
          </router-link>
        </h3>
        <p v-if="l.Error" class="error">{{ l.Error }}</p>
        <Funnel v-else :steps="l.Steps" :names="l.Steps.map((s) => s.Name)" />
      </template>

      <template v-for="b in rep.Breakdowns || []" :key="b.Name">
        <h3>内訳: {{ b.Name }}（全 {{ b.TotalRows }} 行）</h3>
        <p v-if="b.Error" class="error">{{ b.Error }}</p>
        <p v-else-if="!b.Rows || !b.Rows.length">データなし</p>
        <table v-else>
          <thead>
            <tr>
              <th v-for="c in b.Columns || []" :key="c">{{ c }}</th>
              <th v-if="b.Share">share</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in b.Rows" :key="i">
              <td
                v-for="(cell, j) in row"
                :key="j"
                :class="isNum(cell) ? 'n' : ''"
              >
                {{ cell }}
              </td>
              <td v-if="b.Share" class="share-cell">
                <span
                  class="share"
                  :style="{ width: `${b.Share[i] * 120}px` }"
                />{{ pct(b.Share[i]) }}
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TrendChart, { Series } from "./TrendChart.vue";
import Funnel from "./Funnel.vue";
import EndpointPanel from "./EndpointPanel.vue";
import {
  ROLES,
  ROLE_LABEL,
  Report,
  getJSON,
  pct,
  shortHash,
} from "../analysis";

export default defineComponent({
  components: { TrendChart, Funnel, EndpointPanel },
  data() {
    return {
      rep: null as Report | null,
      hovered: -1,
      loading: false,
      error: "",
    };
  },
  computed: {
    gid(): string {
      return this.$route.params.gid as string;
    },
    origin(): string {
      // GroupId と同じ表記（observer のローカル時刻の HH:MM:SS）にする
      return this.gid.substring(11, 19).replace(/-/g, ":");
    },
    points() {
      return (this.rep?.Timeline.OffsetSec || []).map((o) => ({
        label: `${o}s`,
      }));
    },
    charts() {
      const rep = this.rep;
      if (!rep) return [];
      const tl = rep.Timeline;
      // sum / count の系列で値が無い区間は「0件」なので 0 で埋める（埋めないと線が区間を飛び越える）
      const additive = new Set(
        (rep.SeriesStats || [])
          .filter((s) => s.Agg === "sum" || s.Agg === "count")
          .map((s) => s.Name),
      );
      const col = (name: string): Series | null => {
        const s = (tl.Series || []).find((x) => x.Name === name);
        if (!s) return null;
        const fill = additive.has(name.replace(/\(累積\)$/, ""));
        return { name, values: fill ? s.Values.map((v) => v ?? 0) : s.Values };
      };
      const charts: { title: string; unit: string; series: Series[] }[] = [];
      const stats = rep.SeriesStats || [];

      const scoreNames = stats
        .filter((s) => s.Role === "score")
        .map((s) => s.Name);
      if (scoreNames.length) {
        charts.push({
          title: "得点の累積",
          unit: "",
          series: scoreNames
            .map((n) => col(`${n}(累積)`))
            .filter((s): s is Series => !!s),
        });
        charts.push({
          title: "得点（区間ごと）",
          unit: "",
          series: scoreNames.map(col).filter((s): s is Series => !!s),
        });
      }
      // リクエスト総数は下のエンドポイント別の積み上げの棒の高さで見る
      charts.push({
        title: "エラー（区間ごと）",
        unit: "",
        series: [
          { name: "499", values: tl.S499 },
          { name: "5xx", values: tl.S5xx },
          { name: "4xx", values: tl.S4xx },
        ],
      });
      charts.push({
        title: "応答時間",
        unit: "s",
        series: [
          { name: "p50", values: tl.P50Sec },
          { name: "p99", values: tl.P99Sec },
        ],
      });
      for (const role of ROLES.filter((r) => r !== "score")) {
        const names = stats.filter((s) => s.Role === role).map((s) => s.Name);
        const series = names.map(col).filter((s): s is Series => !!s);
        if (series.length) {
          charts.push({ title: `系列: ${ROLE_LABEL[role]}`, unit: "", series });
        }
      }
      // 役割の無い系列（ライフサイクルの開始・完了数など）
      const plain = stats.filter((s) => !s.Role).map((s) => s.Name);
      const lifecycleNames = new Set(
        plain.map((n) => n.replace(/\.(started|finished)$/, "")),
      );
      for (const ln of lifecycleNames) {
        const series = [col(`${ln}.started`), col(`${ln}.finished`)].filter(
          (s): s is Series => !!s,
        );
        if (series.length === 2) {
          charts.push({
            title: `ライフサイクル: ${ln}（開始・完了）`,
            unit: "",
            series,
          });
        } else {
          const s = col(ln);
          if (s) charts.push({ title: `系列: ${ln}`, unit: "", series: [s] });
        }
      }
      return charts;
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
    shortHash,
    async load() {
      this.loading = true;
      this.error = "";
      try {
        this.rep = await getJSON<Report>(`/api/analysis/${this.gid}`);
      } catch (e) {
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
    isNum(v: string) {
      return v !== "" && !isNaN(Number(v));
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

h2 {
  margin: 0.2em 0 0.5em 0;
}

.share-cell {
  white-space: nowrap;
}

.sub-link {
  margin-left: 1em;
  font-size: 0.8em;
  font-weight: normal;
}
</style>
