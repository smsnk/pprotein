<template>
  <section>
    <div class="controls">
      <router-link :to="`/analysis/${gid}/`">← この走行の集計</router-link>
      <label>
        ライフサイクル
        <select v-model="lifecycle" @change="load">
          <option value="">（すべてのセッション）</option>
          <option v-for="l in lifecycles" :key="l.Name" :value="l.Name">
            {{ l.Name }}
          </option>
        </select>
      </label>
      <button @click="load">Reload</button>
      <span v-if="loading">読み込み中 ...</span>
    </div>
    <p v-if="error" class="error">{{ error }}</p>

    <template v-if="sessions">
      <h2>セッションの導線: {{ gid }}</h2>
      <p class="note">
        セッションは nginx のログの cookie（analysis.yml の
        httplog.entity_key）で束ねた利用者です。ログイン要求は cookie
        を持たないので、各セッションはログインの直後から始まります。
      </p>

      <h3>
        よくある遷移（{{ transitions?.Sessions ?? 0 }}
        セッション、件数の多い順）
      </h3>
      <p class="note">
        「from の次に to を叩いた」回数。割合は from の次の要求のうち to
        が占める分。(開始) は最初の要求、(終了) は最後の要求。
      </p>
      <table v-if="transitions">
        <thead>
          <tr>
            <th>from</th>
            <th>to</th>
            <th>件数</th>
            <th>from の次に占める割合</th>
            <th>間隔 p50</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(t, i) in transitions.Transitions" :key="i" class="static">
            <td class="ep">{{ prettyEndpoint(t.From) }}</td>
            <td class="ep">{{ prettyEndpoint(t.To) }}</td>
            <td class="n">{{ t.Count.toLocaleString() }}</td>
            <td class="share-cell">
              <span
                class="share"
                :style="{ width: `${t.ShareOfFrom * 120}px` }"
              />{{ pct(t.ShareOfFrom) }}
            </td>
            <td class="n">
              {{ t.From === START || t.To === END ? "" : sec(t.GapP50Sec) }}
            </td>
          </tr>
        </tbody>
      </table>

      <h3>
        セッション（{{ filtered.length }} /
        {{ sessions.Sessions.length }} 件、全 {{ sessions.Total }} 件中）
      </h3>
      <div class="controls">
        <label v-if="currentSteps.length">
          最後に到達した状態
          <select v-model="lastStep">
            <option value="">（すべて）</option>
            <option v-for="s in currentSteps" :key="s" :value="s">
              {{ s }}
            </option>
          </select>
        </label>
        <span class="note"
          >列の見出しをクリックすると並べ替えます。行をクリックするとそのセッションの導線へ。</span
        >
      </div>
      <table>
        <thead>
          <tr>
            <th
              v-for="c in columns"
              :key="c.key"
              class="sortable"
              @click="sortBy(c.key)"
            >
              {{ c.label
              }}{{ sortKey === c.key ? (sortDesc ? " ▼" : " ▲") : "" }}
            </th>
            <th v-for="l in sessions.Lifecycles || []" :key="l.Name">
              {{ l.Name }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in filtered" :key="s.ID" @click="open(s.ID)">
            <td>{{ s.ID }}</td>
            <td class="n">{{ s.StartSec.toFixed(1) }}s</td>
            <td class="n">{{ (s.EndSec - s.StartSec).toFixed(1) }}s</td>
            <td class="n">{{ s.Requests }}</td>
            <td class="n" :class="{ worse: s.Errors > 0 }">{{ s.Errors }}</td>
            <td class="n" :class="{ worse: s.S499 > 0 }">{{ s.S499 }}</td>
            <td class="n">{{ sec(s.MaxReqSec) }}</td>
            <td class="hosts">{{ (s.Hosts || []).join(" ") }}</td>
            <td v-for="r in s.Lifecycles || []" :key="r.Name">
              {{ r.Reached ? `${r.Reached}: ${r.LastStep}` : "-" }}
            </td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import {
  SessionSummary,
  SessionsResponse,
  TransitionsResponse,
  getJSON,
  pct,
  sec,
  prettyEndpoint,
} from "../analysis";

type SortKey =
  | "ID"
  | "StartSec"
  | "Duration"
  | "Requests"
  | "Errors"
  | "S499"
  | "MaxReqSec"
  | "Hosts";

export default defineComponent({
  data() {
    return {
      sessions: null as SessionsResponse | null,
      transitions: null as TransitionsResponse | null,
      lifecycle: (this.$route.query.lifecycle as string) || "",
      lastStep: "",
      sortKey: "StartSec" as SortKey,
      sortDesc: false,
      loading: false,
      error: "",
      START: "(開始)",
      END: "(終了)",
      columns: [
        { key: "ID", label: "ID" },
        { key: "StartSec", label: "開始" },
        { key: "Duration", label: "長さ" },
        { key: "Requests", label: "要求" },
        { key: "Errors", label: "エラー" },
        { key: "S499", label: "499" },
        { key: "MaxReqSec", label: "最大応答" },
        { key: "Hosts", label: "host" },
      ] as { key: SortKey; label: string }[],
    };
  },
  computed: {
    gid(): string {
      return this.$route.params.gid as string;
    },
    lifecycles() {
      return this.sessions?.Lifecycles || [];
    },
    currentSteps(): string[] {
      return (
        this.lifecycles.find((l) => l.Name === this.lifecycle)?.Steps || []
      );
    },
    filtered(): SessionSummary[] {
      const rows = (this.sessions?.Sessions || []).filter((s) => {
        if (!this.lastStep) return true;
        return (s.Lifecycles || []).some(
          (r) => r.Name === this.lifecycle && r.LastStep === this.lastStep,
        );
      });
      const val = (s: SessionSummary): number | string => {
        switch (this.sortKey) {
          case "Duration":
            return s.EndSec - s.StartSec;
          case "Hosts":
            return (s.Hosts || []).join(" ");
          default:
            return s[this.sortKey];
        }
      };
      const dir = this.sortDesc ? -1 : 1;
      return [...rows].sort((a, b) =>
        val(a) < val(b) ? -dir : val(a) > val(b) ? dir : 0,
      );
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
    prettyEndpoint,
    pct,
    sec,
    async load() {
      this.loading = true;
      this.error = "";
      this.lastStep = "";
      try {
        const q = this.lifecycle
          ? `?lifecycle=${encodeURIComponent(this.lifecycle)}`
          : "";
        const tq = this.lifecycle ? `${q}&top=40` : "?top=40";
        [this.sessions, this.transitions] = await Promise.all([
          getJSON<SessionsResponse>(`/api/analysis/${this.gid}/sessions${q}`),
          getJSON<TransitionsResponse>(
            `/api/analysis/${this.gid}/transitions${tq}`,
          ),
        ]);
      } catch (e) {
        this.error = `${e instanceof Error ? e.message : e}`;
      } finally {
        this.loading = false;
      }
    },
    sortBy(key: SortKey) {
      if (this.sortKey === key) {
        this.sortDesc = !this.sortDesc;
      } else {
        this.sortKey = key;
        this.sortDesc = key !== "ID" && key !== "StartSec" && key !== "Hosts";
      }
    },
    open(id: string) {
      this.$router.push(`/analysis/${this.gid}/sessions/${id}/`);
    },
  },
});
</script>

<style scoped lang="scss">
@import "./analysis.scss";

h2 {
  margin: 0.2em 0 0.5em 0;
}

.sortable {
  cursor: pointer;
  user-select: none;
}

.ep {
  word-break: break-all;
}

.hosts {
  font-size: 0.85em;
  color: #555;
}

.share-cell {
  white-space: nowrap;
}

tr.static {
  cursor: default;
}
</style>
