<script lang="ts">
  import DetailTabs, { type TabItem } from './DetailTabs.svelte';

  interface Props {
    tabs: TabItem<string>[];
    active: string;
    onchange: (id: string) => void;
  }
  let { tabs, active, onchange }: Props = $props();
</script>

<DetailTabs {tabs} {active} {onchange} />
