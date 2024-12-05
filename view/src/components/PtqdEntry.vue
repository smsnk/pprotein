<template>
  <section>
    <a :href="`/api/ptqd/data/${$route.params.id}`" download>Download</a>
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
      const resp = await fetch(`/api/ptqd/${id}`);
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
  white-space: pre-wrap;
  word-wrap: break-word;
  background: #f5f5f5;
  padding: 1em;
  border-radius: 4px;
}
</style> 