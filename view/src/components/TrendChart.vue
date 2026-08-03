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
      <!-- y 軸の目盛り -->
      <g v-for="t in ticks" :key="t.value">
        <line :x1="PAD.l" :x2="W - PAD.r" :y1="t.y" :y2="t.y" class="grid" />
        <text :x="PAD.l - 6" :y="t.y + 4" class="tick">{{ t.label }}</text>
      </g>

      <!-- 系列 -->
      <g v-for="(s, i) in series" :key="s.name">
        <polyline :points="polyline(s.values)" :stroke="color(i)" class="line" />
        <circle
          v-for="(p, j) in dots(s.values)"
          :key="j"
          :cx="p.x"
          :cy="p.y"
          :fill="color(i)"
          r="3"
        />
      </g>

      <!-- ホバー位置 -->
      <line
        v-if="hovered >= 0"
        :x1="x(hovered)"
        :x2="x(hovered)"
        :y1="PAD.t"
        :y2="H - PAD.b"
        class="cursor"
      />

      <!-- x 軸: 各点がどのコミットかを出す -->
      <text
        v-for="(label, i) in xLabels"
        :key="i"
        :x="x(i)"
        :y="H - 6"
        :class="['xlabel', { active: i === hovered }]"
      >
        {{ label }}
      </text>
    </svg>
    <p v-if="!hasData" class="empty">データがありません</p>
  </figure>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";

export interface Series {
  name: string;
  values: (number | null)[];
}

const PALETTE = [
  "#2c6fbb",
  "#c0392b",
  "#27ae60",
  "#8e44ad",
  "#e67e22",
  "#16a085",
];

export default defineComponent({
  props: {
    title: { type: String, required: true },
    points: { type: Array as PropType<{ label: string }[]>, required: true },
    series: { type: Array as PropType<Series[]>, required: true },
    hovered: { type: Number, default: -1 },
    unit: { type: String, default: "" },
  },
  emits: ["hover"],
  data() {
    return {
      W: 900,
      H: 220,
      PAD: { l: 64, r: 48, t: 12, b: 26 },
    };
  },
  computed: {
    values(): number[] {
      return this.series
        .flatMap((s) => s.values)
        .filter((v): v is number => v !== null && !isNaN(v));
    },
    hasData(): boolean {
      return this.values.length > 0;
    },
    max(): number {
      const m = Math.max(0, ...this.values);
      return m === 0 ? 1 : m * 1.05;
    },
    // 点が多いときはラベルを間引く。ホバー中の点は必ず出す。
    xLabels(): string[] {
      const step = Math.ceil(this.points.length / 10) || 1;
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
  },
  methods: {
    color(i: number) {
      return PALETTE[i % PALETTE.length];
    },
    x(i: number) {
      const n = this.points.length;
      if (n <= 1) {
        return (this.PAD.l + this.W - this.PAD.r) / 2;
      }
      return (
        this.PAD.l + ((this.W - this.PAD.l - this.PAD.r) * i) / (n - 1)
      );
    },
    y(v: number) {
      const h = this.H - this.PAD.t - this.PAD.b;
      return this.PAD.t + h - (h * v) / this.max;
    },
    dots(values: (number | null)[]) {
      return values
        .map((v, i) => ({ v, i }))
        .filter(({ v }) => v !== null && !isNaN(v as number))
        .map(({ v, i }) => ({ x: this.x(i), y: this.y(v as number) }));
    },
    polyline(values: (number | null)[]) {
      return this.dots(values)
        .map((p) => `${p.x},${p.y}`)
        .join(" ");
    },
    format(v: number) {
      if (this.unit === "s") {
        return v >= 1 ? `${v.toFixed(1)}s` : `${(v * 1000).toFixed(0)}ms`;
      }
      if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`;
      if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
      return v.toFixed(v < 10 ? 1 : 0);
    },
    onMove(e: MouseEvent) {
      const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
      const ratio = this.W / rect.width;
      const px = (e.clientX - rect.left) * ratio;
      let best = -1;
      let bestDist = Infinity;
      for (let i = 0; i < this.points.length; i++) {
        const d = Math.abs(this.x(i) - px);
        if (d < bestDist) {
          bestDist = d;
          best = i;
        }
      }
      this.$emit("hover", best);
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
  height: 220px;
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

.line {
  fill: none;
  stroke-width: 2;
}

.cursor {
  stroke: #999;
  stroke-dasharray: 3 3;
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

.empty {
  color: #888;
}
</style>
