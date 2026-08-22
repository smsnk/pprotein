<template>
  <section>
    <PproteinForm v-if="$props.collectable" :endpoint="$props.endpoint" />
    <EntriesTable :entries="$store.getters.entriesByType($props.endpoint)" />
  </section>
</template>

<script lang="ts">
import EntriesTable from "./EntriesTable.vue";
import PproteinForm from "./PproteinForm.vue";
import { defineComponent } from "vue";

export default defineComponent({
  components: {
    EntriesTable,
    PproteinForm,
  },
  props: {
    endpoint: {
      type: String,
      required: true,
    },
    // score のように POST の形式が違い、URL/Duration の収集フォームが使えないタイプでは false にする
    collectable: {
      type: Boolean,
      default: true,
    },
  },
});
</script>

<style scoped lang="scss">
section {
  margin: 2em;
}
</style>
