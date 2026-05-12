<script lang="ts">
  import WorktreeModal from './WorktreeModal.svelte';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
  }
  let { site }: Props = $props();
</script>

<WorktreeModal {site} />
