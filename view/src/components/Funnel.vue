<template>
  <table class="funnel">
    <thead>
      <tr>
        <th>状態</th>
        <th class="bar-col">
          到達{{ after ? "（上: before / 下: after）" : "" }}
        </th>
        <th>到達率</th>
        <th>止まった</th>
        <th>1件あたり</th>
        <th>所要 p50 / p90</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="row in rows" :key="row.name">
        <td>{{ row.name }}</td>
        <td class="bar-col">
          <div v-for="(s, i) in row.sides" :key="i" class="bar-line">
            <div
              class="bar"
              :class="i === 0 && after ? 'before' : 'after'"
              :style="{ width: `${(100 * (s?.Reached ?? 0)) / max}%` }"
            />
            <span class="bar-label">{{
              s ? s.Reached.toLocaleString() : "（なし）"
            }}</span>
          </div>
        </td>
        <td class="n">
          <div v-for="(s, i) in row.sides" :key="i">
            {{ s ? pct(s.RateFromPrev) : "-" }}
          </div>
        </td>
        <td class="n">
          <div v-for="(s, i) in row.sides" :key="i">
            {{ s ? s.StoppedHere : "-" }}
          </div>
        </td>
        <td class="n">
          <div v-for="(s, i) in row.sides" :key="i">
            {{ s ? s.PerEntity.toFixed(1) : "-" }}
          </div>
        </td>
        <td class="n">
          <div v-for="(s, i) in row.sides" :key="i">
            {{
              s?.FromPrevSec
                ? `${sec(s.FromPrevSec.P50)} / ${sec(s.FromPrevSec.P90)}`
                : "-"
            }}
          </div>
        </td>
      </tr>
    </tbody>
  </table>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import { StepResult, pct, sec } from "../analysis";

// 状態ごとの到達数を横棒で並べる。after を渡すと before / after を上下に並べて比べる。
export default defineComponent({
  props: {
    steps: {
      type: Array as PropType<(StepResult | undefined)[]>,
      required: true,
    },
    names: { type: Array as PropType<string[]>, required: true },
    after: {
      type: Array as PropType<(StepResult | undefined)[] | null>,
      default: null,
    },
  },
  computed: {
    rows() {
      return this.names.map((name, i) => ({
        name,
        sides: this.after ? [this.steps[i], this.after[i]] : [this.steps[i]],
      }));
    },
    max(): number {
      const all = [...this.steps, ...(this.after ?? [])].map(
        (s) => s?.Reached ?? 0,
      );
      return Math.max(1, ...all);
    },
  },
  methods: { pct, sec },
});
</script>

<style scoped lang="scss">
.funnel {
  border-collapse: collapse;
  width: 100%;
  margin-bottom: 1.5em;
}

th,
td {
  padding: 0.3em 0.8em;
  border-bottom: 1px solid #ddd;
  text-align: left;
  vertical-align: middle;
}

.bar-col {
  width: 40%;
}

.bar-line {
  display: flex;
  align-items: center;
  gap: 0.5em;
  height: 1.3em;
}

.bar {
  height: 0.9em;
  min-width: 1px;
  border-radius: 0.15em;

  &.before {
    background: #b0b8c4;
  }

  &.after {
    background: #2c6fbb;
  }
}

.bar-label {
  font-size: 0.9em;
  white-space: nowrap;
}

.n {
  text-align: right;
  white-space: nowrap;
}
</style>
