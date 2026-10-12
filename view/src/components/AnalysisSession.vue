<template>
  <section>
    <div class="controls">
      <router-link :to="`/analysis/${gid}/sessions/`"
        >← セッション一覧</router-link
      >
      <router-link v-if="prevId" :to="`/analysis/${gid}/sessions/${prevId}/`"
        >‹ {{ prevId }}</router-link
      >
      <router-link v-if="nextId" :to="`/analysis/${gid}/sessions/${nextId}/`"
        >{{ nextId }} ›</router-link
      >
      <span v-if="loading">読み込み中 ...</span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="d">
      <h2>セッション {{ d.Summary.ID }}</h2>
      <div class="detail">
        {{ d.Summary.StartSec.toFixed(1) }}s 〜
        {{ d.Summary.EndSec.toFixed(1) }}s（{{
          (d.Summary.EndSec - d.Summary.StartSec).toFixed(1)
        }}s）/ 要求 {{ d.Summary.Requests }} / エラー {{ d.Summary.Errors }} /
        499 {{ d.Summary.S499 }} / 最大応答
        {{ sec(d.Summary.MaxReqSec) }}
        <div class="commit">
          host: {{ (d.Summary.Hosts || []).join(", ") || "-" }}
        </div>
        <div class="commit">
          <span
            v-for="r in d.Summary.Lifecycles || []"
            :key="r.Name"
            class="reach"
          >
            {{ r.Name }}:
            {{
              r.Reached
                ? `${r.Reached} 段目（${r.LastStep}）まで`
                : "入っていない"
            }}
          </span>
        </div>
      </div>

      <SessionChart :requests="d.Requests" />

      <h3>要求の列</h3>
      <table>
        <thead>
          <tr>
            <th>#</th>
            <th>t</th>
            <th>前から</th>
            <th>要求</th>
            <th>host</th>
            <th>status</th>
            <th>応答</th>
            <th>進んだ状態</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(r, i) in d.Requests"
            :key="i"
            class="static"
            :class="{ step: r.Steps?.length }"
          >
            <td class="n">{{ i + 1 }}</td>
            <td class="n">{{ r.OffsetSec.toFixed(3) }}s</td>
            <td class="n">
              {{
                i
                  ? `+${(r.OffsetSec - d.Requests[i - 1].OffsetSec).toFixed(
                      3,
                    )}s`
                  : ""
              }}
            </td>
            <td class="uri">{{ r.Method }} {{ r.URI }}</td>
            <td class="hosts">{{ r.Host }}</td>
            <td class="n" :style="{ color: statusColor(r.Status) }">
              {{ r.Status }}
            </td>
            <td class="n" :class="{ worse: r.ReqTimeSec >= 1 }">
              {{ sec(r.ReqTimeSec) }}
            </td>
            <td>{{ (r.Steps || []).join(", ") }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import SessionChart from "./SessionChart.vue";
import { SessionDetail, getJSON, sec, statusColor } from "../analysis";

export default defineComponent({
  components: { SessionChart },
  data() {
    return {
      d: null as SessionDetail | null,
      loading: false,
      error: "",
    };
  },
  computed: {
    gid(): string {
      return this.$route.params.gid as string;
    },
    sid(): string {
      return this.$route.params.sid as string;
    },
    // 連番なので前後のセッションへ移れる
    prevId(): string {
      const n = parseInt(this.sid.slice(1), 10);
      return n > 1 ? `s${String(n - 1).padStart(3, "0")}` : "";
    },
    nextId(): string {
      const n = parseInt(this.sid.slice(1), 10);
      return isNaN(n) ? "" : `s${String(n + 1).padStart(3, "0")}`;
    },
  },
  watch: {
    sid() {
      this.load();
    },
  },
  async created() {
    await this.load();
  },
  methods: {
    sec,
    statusColor,
    async load() {
      this.loading = true;
      this.error = "";
      try {
        this.d = await getJSON<SessionDetail>(
          `/api/analysis/${this.gid}/sessions/${this.sid}`,
        );
      } catch (e) {
        this.d = null;
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

h2 {
  margin: 0.2em 0 0.5em 0;
}

.reach {
  margin-right: 1.5em;
}

.uri {
  word-break: break-all;
}

.hosts {
  font-size: 0.85em;
  color: #555;
}

tr.static {
  cursor: default;
}

tr.step {
  background: #fff7e6;
}
</style>
