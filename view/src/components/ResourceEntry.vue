<template>
  <section>
    <a :href="`/api/resource/data/${$route.params.id}`" download>
      Download (raw samples)
    </a>
    <pre>{{ content }}</pre>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";

export default defineComponent({
  data() {
    return {
      content: "Loading...",
    };
  },
  async created() {
    await this.updateContent(this.$route.params.id);
  },
  async beforeRouteUpdate(route) {
    await this.updateContent(route.params.id);
  },
  methods: {
    async updateContent(id: string | string[]) {
      const resp = await fetch(`/api/resource/${id}`);
      this.content = await resp.text();
    },
  },
});
</script>

<style scoped lang="scss">
section {
  padding: 2em;
}

pre {
  white-space: pre;
  overflow-x: auto;
  background: #f5f5f5;
  padding: 1em;
  border-radius: 4px;
}
</style>
