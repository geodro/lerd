<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import { apiBase } from '$lib/api';
  import type { Site } from '$stores/sites';

  interface Props {
    site: Site;
    size?: string;
  }
  let { site, size = 'w-4 h-4' }: Props = $props();
</script>

{#if site.custom_container}
  <svg class="{size} {site.fpm_running ? 'text-violet-500' : 'text-gray-300 dark:text-gray-600'}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>
  </svg>
{:else if site.has_favicon}
  <img src={apiBase + '/api/sites/' + site.domain + '/favicon'} class="{size} rounded-xs object-contain" loading="lazy" alt="" />
{:else}
  <StatusDot color={site.fpm_running ? 'green' : 'gray'} />
{/if}
