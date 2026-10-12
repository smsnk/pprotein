<template>
  <figure>
    <figcaption>
      {{ title }}
      <span v-for="(s, i) in series" :key="s.name" class="legend">
        <i :style="{ backgroundColor: color(i) }" />{{ s.name }}
      </span>
    </figcaption>
    <svg
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
      @mousemove="onMove"
      @mouseleave="$emit('hover', -1)"
    >
      <g v-for="t in ticks" :key="t.value">
        <line :x1="PAD.l" :x2="W - PAD.r" :y1="t.y" :y2="t.y" class="grid" />
        <text :x="PAD.l - 6" :y="t.y + 4" class="tick">{{ t.label }}</text>
      </g>
      <g v-for="(stack, i) in stacks" :key="i">
        <rect
          v-for="seg in stack"
          :key="seg.name"
          :x="barX(i)"
          :y="y(seg.top)"
          :width="barW"
          :height="Math.max(0, y(seg.bottom) - y(seg.top))"
          :fill="seg.color"
          :class="{ dim: hovered >= 0 && hovered !== i }"
        />
      </g>
      <text
        v-for="(label, i) in xLabels"
        :key="`x${i}`"
        :x="barX(i) + barW / 2"
        :y="H - 6"
        :class="['xlabel', { active: i === hovered }]"
      >
        {{ label }}
      </text>
    </svg>

    <!-- 棒の中身を数字で出す。カーソルが無いときは最後の棒 -->
    <table v-if="detail" class="detail-table">
      <thead>
        <tr>
          <th>{{ detail.label }}</th>
          <th>件数</th>
          <th>構成比</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in detail.rows" :key="row.name">
          <td>
            <i class="swatch" :style="{ backgroundColor: row.color }" />{{
              row.name
            }}
          </td>
          <td class="n">{{ row.value.toLocaleString() }}</td>
          <td class="n">{{ pct(row.share) }}</td>
        </tr>
        <tr class="total">
          <td>合計</td>
          <td class="n">{{ detail.total.toLocaleString() }}</td>
          <td class="n">100.0%</td>
        </tr>
      </tbody>
    </table>
  </figure>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import { pct } from "../analysis";

export interface StackSeries {
  name: string;
  values: number[];
}

// TrendChart と同じ並び。「その他」（OTHER）は灰色にして、選んだものを目立たせる。
const PALETTE = [
  "#2c6fbb",
  "#c0392b",
  "#27ae60",
  "#8e44ad",
  "#e67e22",
  "#16a085",
  "#d4ac0d",
  "#7f3f98",
];
export const OTHER = "その他";

// 積み上げ棒。棒の高さ = 全系列の合計、区分の厚み = 各系列の値。
export default defineComponent({
  props: {
    title: { type: String, required: true },
    points: { type: Array as PropType<{ label: string }[]>, required: true },
    series: { type: Array as PropType<StackSeries[]>, required: true },
    hovered: { type: Number, default: -1 },
  },
  emits: ["hover"],
  data() {
    return {
      W: 900,
      H: 240,
      PAD: { l: 64, r: 20, t: 12, b: 26 },
    };
  },
  computed: {
    totals(): number[] {
      return this.points.map((_, i) =>
        this.series.reduce((sum, s) => sum + (s.values[i] ?? 0), 0),
      );
    },
    max(): number {
      const m = Math.max(0, ...this.totals);
      return m === 0 ? 1 : m * 1.05;
    },
    // 下から系列の順に積む
    stacks() {
      return this.points.map((_, i) => {
        let acc = 0;
        return this.series.map((s, j) => {
          const v = s.values[i] ?? 0;
          const seg = {
            name: s.name,
            bottom: acc,
            top: acc + v,
            color: this.color(j),
          };
          acc += v;
          return seg;
        });
      });
    },
    barW(): number {
      const n = Math.max(1, this.points.length);
      const slot = (this.W - this.PAD.l - this.PAD.r) / n;
      return Math.max(2, slot * 0.7);
    },
    xLabels(): string[] {
      const step = Math.ceil(this.points.length / 12) || 1;
      return this.points.map((p, i) =>
        i === this.hovered || i % step === 0 ? p.label : "",
      );
    },
    ticks() {
      const n = 4;
      return Array.from({ length: n + 1 }, (_, i) => {
        const value = (this.max / n) * i;
        return { value, y: this.y(value), label: this.format(value) };
      });
    },
    detail() {
      const i = this.hovered >= 0 ? this.hovered : this.points.length - 1;
      if (i < 0 || !this.points[i]) return null;
      const total = this.totals[i];
      return {
        label: this.points[i].label,
        total,
        // 上に積んだものから並べる（棒の見た目と同じ順）
        rows: this.series
          .map((s, j) => ({
            name: s.name,
            value: s.values[i] ?? 0,
            share: total ? (s.values[i] ?? 0) / total : 0,
            color: this.color(j),
          }))
          .reverse(),
      };
    },
  },
  methods: {
    pct,
    color(i: number) {
      return this.series[i]?.name === OTHER
        ? "#c8c8c8"
        : PALETTE[i % PALETTE.length];
    },
    barX(i: number) {
      const n = Math.max(1, this.points.length);
      const slot = (this.W - this.PAD.l - this.PAD.r) / n;
      return this.PAD.l + slot * i + (slot - this.barW) / 2;
    },
    y(v: number) {
      const h = this.H - this.PAD.t - this.PAD.b;
      return this.PAD.t + h - (h * v) / this.max;
    },
    format(v: number) {
      if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`;
      if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
      return v.toFixed(0);
    },
    onMove(e: MouseEvent) {
      const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
      const px = (e.clientX - rect.left) * (this.W / rect.width);
      const n = this.points.length;
      if (!n) return;
      const slot = (this.W - this.PAD.l - this.PAD.r) / n;
      const i = Math.floor((px - this.PAD.l) / slot);
      this.$emit("hover", Math.min(n - 1, Math.max(0, i)));
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
    border-radius: 0.15em;
  }
}

svg {
  width: 100%;
  height: 240px;
  background: #fafafa;
  border: 1px solid #ddd;
}

.grid {
  stroke: #e0e0e0;
  stroke-width: 1;
}

.tick {
  font-size: 11px;
  fill: #666;
  text-anchor: end;
}

.dim {
  opacity: 0.45;
}

.xlabel {
  font-size: 10px;
  fill: #888;
  text-anchor: middle;

  &.active {
    fill: #000;
    font-weight: bold;
  }
}

.detail-table {
  border-collapse: collapse;
  margin-top: 0.5em;
  font-size: 0.9em;

  th,
  td {
    padding: 0.2em 0.8em;
    border-bottom: 1px solid #e6e6e6;
    text-align: left;
  }

  .n {
    text-align: right;
  }

  .total td {
    font-weight: bold;
    border-top: 1px solid #999;
  }
}

.swatch {
  display: inline-block;
  width: 0.8em;
  height: 0.8em;
  margin-right: 0.4em;
  border-radius: 0.15em;
  vertical-align: middle;
}
</style>
