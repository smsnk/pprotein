<template>
  <section>
    <table v-if="score">
      <tbody>
        <tr>
          <th>Score</th>
          <td class="score">{{ score.Score.toLocaleString() }}</td>
        </tr>
        <tr>
          <th>Result</th>
          <td :class="score.Passed ? 'pass' : 'fail'">
            {{ score.Passed ? "pass" : "FAIL" }}
          </td>
        </tr>
        <tr v-if="score.ErrorCount">
          <th>Errors</th>
          <td>{{ score.ErrorCount }}</td>
        </tr>
        <tr v-if="score.Target">
          <th>Target</th>
          <td>{{ score.Target }}</td>
        </tr>
        <tr v-if="duration">
          <th>Duration</th>
          <td>{{ duration }}</td>
        </tr>
      </tbody>
    </table>
    <pre v-if="score && score.Raw">{{ score.Raw }}</pre>
    <p v-if="!score">{{ message }}</p>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";

interface Score {
  Score: number;
  Passed: boolean;
  Target: string;
  ErrorCount: number;
  StartedAt: string;
  FinishedAt: string;
  Raw: string;
}

export default defineComponent({
  data() {
    return {
      message: "Loading ...",
      score: null as Score | null,
    };
  },
  computed: {
    duration(): string {
      const s = this.score;
      if (!s?.StartedAt || !s?.FinishedAt) {
        return "";
      }
      const sec =
        (new Date(s.FinishedAt).getTime() - new Date(s.StartedAt).getTime()) /
        1000;
      return sec > 0 ? `${sec.toFixed(0)}s` : "";
    },
  },
  async created() {
    await this.updateScore(this.$route.params.id);
  },
  async beforeRouteUpdate(route) {
    await this.updateScore(route.params.id);
  },
  methods: {
    async updateScore(id: string | string[]) {
      try {
        const resp = await fetch(`/api/score/${id}`);
        if (!resp.ok) {
          this.message = `http error: status=${resp.status}`;
          return;
        }
        this.score = await resp.json();
      } catch (e) {
        this.message = `Error: ${e instanceof Error ? e.message : e}`;
      }
    },
  },
});
</script>

<style scoped lang="scss">
section {
  padding: 2em;
}

table {
  border-collapse: collapse;
  margin-bottom: 1em;
}

th,
td {
  padding: 0.5em 2em;
  border: 1px solid #999;
  text-align: left;
}

.score {
  font-size: 1.6em;
}

.pass {
  color: blue;
}

.fail {
  color: red;
}

pre {
  white-space: pre-wrap;
  word-wrap: break-word;
  background: #f5f5f5;
  padding: 1em;
  border-radius: 4px;
}
</style>
