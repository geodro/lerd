<script lang="ts">
  import type { Site } from '$stores/sites';
  interface Props {
    site: Site;
    domain?: string;
    onSaved?: () => void;
  }
  let { site, domain, onSaved = () => {} }: Props = $props();
</script>

<div data-testid="nginx-editor-stub">{domain || site.domain}</div>
<button data-testid="stub-saved" onclick={() => onSaved()}>save</button>
