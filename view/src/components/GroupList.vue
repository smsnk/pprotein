<template>
  <section>
    <div class="form">
      <label>
        Webhook URL<br />
        <input v-model="url" type="text" size="50" disabled />
      </label>
      <label>
        &nbsp;<br />
        <button @click="collect">Collect</button>
      </label>
      <label>
        &nbsp;<br />
        <button @click="openAll = !openAll">
          {{ openAll ? "Close" : "Open" }} all
        </button>
      </label>
    </div>
    <details
      v-for="(group, i) in $store.state.groups"
      :key="group"
      :open="i === 0 || openAll"
    >
      <summary>
        Group: {{ group }}
        <span
          v-if="$store.getters.scoreByGroup(group)"
          :class="['score', scoreClass(group)]"
        >
          {{ $store.getters.scoreByGroup(group).Message }}
        </span>
      </summary>
      <GroupEntriesTable
        :group-id="group"
        :entries="$store.getters.entriesByGroup(group)"
      />
      <AddMemo :group-id="group" />
    </details>
    <div v-if="!$store.state.groups.length">No entries!!</div>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import GroupEntriesTable from "./GroupEntriesTable.vue";
import AddMemo from "./AddMemo.vue";

export default defineComponent({
  components: {
    GroupEntriesTable,
    AddMemo,
  },
  data() {
    return {
      openAll: false,
    };
  },
  computed: {
    url() {
      return `${location.origin}/api/group/collect`;
    },
  },
  methods: {
    scoreClass(group: string) {
      const message = this.$store.getters.scoreByGroup(group)?.Message || "";
      return message.includes("FAIL") ? "fail" : "pass";
    },
    async collect() {
      const resp = await fetch(this.url);
      if (!resp.ok) {
        return alert(
          `http error: status=${resp.status}, message=${await resp.text()}`
        );
      }
    },
  },
});
</script>

<style scoped lang="scss">
section {
  margin: 2em;
}

details {
  margin-bottom: 1em;
}

.score {
  margin-left: 1em;
  padding: 0.1em 0.6em;
  border-radius: 0.2em;
  color: #fff;

  &.pass {
    background-color: #2c6fbb;
  }
  &.fail {
    background-color: #c0392b;
  }
}

.form {
  display: flex;
  margin-bottom: 2em;

  label {
    margin-right: 1em;

    &:focus-within {
      color: orangered;
    }

    input {
      border: 1px solid lightgray;
      padding: 0.4em 1em;

      &:focus {
        border-color: orangered;
        outline: 0;
      }
    }
  }
}
</style>
