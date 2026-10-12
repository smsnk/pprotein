<template>
  <figure>
    <figcaption>
      導線（横: 収集開始からの秒、縦:
      エンドポイント。点は応答を返した時刻、横線は処理にかかった時間）
      <span v-for="l in legend" :key="l.label" class="legend"
        ><i :style="{ backgroundColor: l.color }" />{{ l.label }}</span
      >
      <span class="legend"><b class="ring" />ライフサイクルが進んだ要求</span>
    </figcaption>
    <svg :viewBox="`0 0 ${W} ${H}`" :style="{ height: `${H}px` }">
      <g v-for="(row, i) in rows" :key="row">
        <line
          :x1="LABEL_W"
          :x2="W - PAD_R"
          :y1="y(i)"
          :y2="y(i)"
          class="grid"
        />
        <text :x="LABEL_W - 8" :y="y(i) + 4" class="row-label">
          {{ shorten(prettyEndpoint(row)) }}
        </text>
      </g>
      <g v-for="t in ticks" :key="t">
        <line
          :x1="x(t)"
          :x2="x(t)"
          :y1="PAD_T - 6"
          :y2="H - PAD_B"
          class="vgrid"
        />
        <text :x="x(t)" :y="H - 6" class="tick">{{ t }}s</text>
      </g>
      <polyline :points="path" class="path" />
      <line
        v-for="(r, i) in slow"
        :key="`d${i}`"
        :x1="x(r.OffsetSec - r.ReqTimeSec)"
        :x2="x(r.OffsetSec)"
        :y1="y(rowOf(r))"
        :y2="y(rowOf(r))"
        :stroke="statusColor(r.Status)"
        class="duration"
      />
      <g v-for="(r, i) in requests" :key="i">
        <circle
          v-if="r.Steps?.length"
          :cx="x(r.OffsetSec)"
          :cy="y(rowOf(r))"
          r="7"
          class="ring-mark"
        />
        <circle
          :cx="x(r.OffsetSec)"
          :cy="y(rowOf(r))"
          r="3.5"
          :fill="statusColor(r.Status)"
        >
          <title>
            {{ r.OffsetSec.toFixed(3) }}s {{ r.Method }} {{ r.URI }} →
            {{ r.Status }} ({{ sec(r.ReqTimeSec) }}){{
              r.Steps?.length ? ` [${r.Steps.join(", ")}]` : ""
            }}
          </title>
        </circle>
      </g>
    </svg>
  </figure>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import { SessionRequest, sec, statusColor, prettyEndpoint } from "../analysis";

const ROW_H = 24;

// 1セッションの要求を、横軸に時刻・縦軸にエンドポイントを取って点で並べる。
// 縦の並びは初めて叩いた順（上から導線の順になる）。
export default defineComponent({
  props: {
    requests: { type: Array as PropType<SessionRequest[]>, required: true },
  },
  data() {
    return {
      W: 1100,
      LABEL_W: 330,
      PAD_R: 20,
      PAD_T: 16,
      PAD_B: 28,
      legend: [
        { label: "2xx", color: statusColor(200) },
        { label: "3xx", color: statusColor(304) },
        { label: "4xx", color: statusColor(400) },
        { label: "499", color: statusColor(499) },
        { label: "5xx", color: statusColor(500) },
      ],
    };
  },
  computed: {
    rows(): string[] {
      const seen: string[] = [];
      this.requests.forEach((r) => {
        const k = `${r.Method} ${r.Endpoint}`;
        if (!seen.includes(k)) seen.push(k);
      });
      return seen;
    },
    H(): number {
      return this.PAD_T + this.rows.length * ROW_H + this.PAD_B;
    },
    span(): [number, number] {
      if (!this.requests.length) return [0, 1];
      const ts = this.requests.flatMap((r) => [
        r.OffsetSec,
        r.OffsetSec - r.ReqTimeSec,
      ]);
      const lo = Math.floor(Math.min(...ts));
      const hi = Math.ceil(Math.max(...ts));
      return [lo, hi === lo ? lo + 1 : hi];
    },
    ticks(): number[] {
      const [lo, hi] = this.span;
      const step =
        [1, 2, 5, 10, 20, 30, 60].find((s) => (hi - lo) / s <= 12) ?? 60;
      const out: number[] = [];
      for (let t = Math.ceil(lo / step) * step; t <= hi; t += step) out.push(t);
      return out;
    },
    // 処理時間を横線で描く要求（短いものは点と重なって見えないので省く）
    slow(): SessionRequest[] {
      return this.requests.filter((r) => r.ReqTimeSec >= 0.2);
    },
    path(): string {
      return this.requests
        .map((r) => `${this.x(r.OffsetSec)},${this.y(this.rowOf(r))}`)
        .join(" ");
    },
  },
  methods: {
    prettyEndpoint,
    sec,
    statusColor,
    x(t: number) {
      const [lo, hi] = this.span;
      return (
        this.LABEL_W +
        ((this.W - this.LABEL_W - this.PAD_R) * (t - lo)) / (hi - lo)
      );
    },
    y(i: number) {
      return this.PAD_T + i * ROW_H + ROW_H / 2;
    },
    rowOf(r: SessionRequest) {
      return this.rows.indexOf(`${r.Method} ${r.Endpoint}`);
    },
    shorten(s: string) {
      return s.length > 50 ? `${s.slice(0, 49)}…` : s;
    },
  },
});
</script>

<style scoped lang="scss">
figure {
  margin: 0 0 2em 0;
}

figcaption {
  margin-bottom: 0.4em;
  font-weight: bold;
}

.legend {
  margin-left: 1em;
  font-weight: normal;
  font-size: 0.9em;

  i {
    display: inline-block;
    width: 0.8em;
    height: 0.8em;
    margin-right: 0.3em;
    border-radius: 50%;
  }

  .ring {
    display: inline-block;
    width: 0.9em;
    height: 0.9em;
    margin-right: 0.3em;
    border: 2px solid #f39c12;
    border-radius: 50%;
    vertical-align: middle;
  }
}

svg {
  width: 100%;
  background: #fafafa;
  border: 1px solid #ddd;
}

.grid {
  stroke: #e6e6e6;
}

.vgrid {
  stroke: #eee;
  stroke-dasharray: 2 3;
}

.row-label {
  font-size: 11px;
  fill: #444;
  text-anchor: end;
}

.tick {
  font-size: 10px;
  fill: #888;
  text-anchor: middle;
}

.path {
  fill: none;
  stroke: #c8c8c8;
  stroke-width: 1;
}

.duration {
  stroke-width: 3;
  opacity: 0.45;
}

.ring-mark {
  fill: none;
  stroke: #f39c12;
  stroke-width: 2;
}
</style>
