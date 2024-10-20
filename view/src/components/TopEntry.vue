<template>
  <section>
    <div>
      <button @click="toggleYAxisScale">Y軸スケール切替: {{ yAxisScaleMode }}</button>
    </div>
    <div ref="cpuChartContainer" class="chart-container"></div>
    <div ref="memChartContainer" class="chart-container"></div>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import * as echarts from "echarts";

export default defineComponent({
  data() {
    return {
      cpuChart: null as echarts.ECharts | null,
      memChart: null as echarts.ECharts | null,
      topData: [] as any[],
      yAxisScaleMode: "全時間" as "全時間" | "表示範囲",
    };
  },
  async created() {
    await this.fetchData(this.$route.params.id as string);
    this.initCharts();
  },
  methods: {
    async fetchData(id: string) {
      const response = await fetch(`/api/top/data/${id}`);
      this.topData = await response.json();
      this.updateCharts();
    },
    initCharts() {
      if (this.$refs.cpuChartContainer && this.$refs.memChartContainer) {
        this.cpuChart = echarts.init(this.$refs.cpuChartContainer as HTMLElement);
        this.memChart = echarts.init(this.$refs.memChartContainer as HTMLElement);
        this.updateCharts();
      }
    },
    updateCharts() {
      if (!this.cpuChart || !this.memChart || this.topData.length === 0) return;

      const times = this.topData.map(entry => entry.time);
      const { cpuSeries, memSeries } = this.getSeries();

      const cpuOption = this.getChartOption("CPU Usage", times, cpuSeries);
      const memOption = this.getChartOption("Memory Usage", times, memSeries);

      this.cpuChart.setOption(cpuOption);
      this.memChart.setOption(memOption);
    },
    getChartOption(title: string, times: string[], series: any[]) {
      const option = {
        title: { text: title },
        tooltip: { trigger: "axis", axisPointer: { type: "cross" } },
        legend: { data: series.map(s => s.name) },
        xAxis: { 
          type: "category", 
          data: times,
          axisLabel: {
            rotate: 45,
            interval: 'auto'
          }
        },
        yAxis: { 
          type: "value",
          max: this.getYAxisMax(series),
        },
        series: series,
        dataZoom: [
          {
            type: 'slider',
            show: true,
            xAxisIndex: [0],
            start: 0,
            end: 100
          },
          {
            type: 'slider',
            show: true,
            yAxisIndex: [0],
            left: '93%',
            start: 0,
            end: 100
          },
          {
            type: 'inside',
            xAxisIndex: [0],
            start: 0,
            end: 100
          },
          {
            type: 'inside',
            yAxisIndex: [0],
            start: 0,
            end: 100
          }
        ],
        toolbox: {
          feature: {
            dataZoom: {
              yAxisIndex: 'none'
            },
            restore: {},
            saveAsImage: {}
          }
        }
      };
      return option;
    },
    getYAxisMax(series: any[]) {
      if (this.yAxisScaleMode === "全時間") {
        return Math.ceil(Math.max(...series.flatMap(s => s.data)));
      }
      return 'dataMax';
    },
    toggleYAxisScale() {
      this.yAxisScaleMode = this.yAxisScaleMode === "全時間" ? "表示範囲" : "全時間";
      this.updateCharts();
    },
    getSeries() {
      const processes = ["pprotein", "top", "bash"];
      const cpuSeries = processes.map(process => ({
        name: process,
        type: "bar",
        stack: "total",
        data: this.topData.map(entry => {
          const processData = entry.data.find((d: any) => d.values.COMMAND === process);
          return processData ? parseFloat(processData.values["%CPU"]) : 0;
        }),
      }));
      const memSeries = processes.map(process => ({
        name: process,
        type: "bar",
        stack: "total",
        data: this.topData.map(entry => {
          const processData = entry.data.find((d: any) => d.values.COMMAND === process);
          return processData ? parseFloat(processData.values["%MEM"]) : 0;
        }),
      }));
      return { cpuSeries, memSeries };
    },
  },
});
</script>

<style scoped>
.chart-container {
  width: 100%;
  height: 500px;
  margin-bottom: 30px;
}
</style>
