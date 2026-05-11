<script lang="ts">
  import { onMount } from 'svelte';
  import { status, refreshStatus, toggleDumps } from '$stores/dumps';

  // Compact antenna-style toggle for the Sites list header. Click to flip
  // the lerd dump bridge on/off — when on, every PHP-FPM container has
  // dump()/dd() shipped into the dashboard. A pulsing emerald dot in the
  // top-right corner advertises that the bridge is currently active so
  // the user notices captures without opening a Dumps tab.

  let busy = $state(false);
  const enabled = $derived(Boolean($status?.enabled));

  onMount(() => {
    void refreshStatus();
  });

  async function onclick(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    if (busy) return;
    busy = true;
    try {
      await toggleDumps(!enabled);
      await refreshStatus();
    } finally {
      busy = false;
    }
  }

  const title = $derived(
    busy ? 'Toggling dump bridge…' : enabled ? 'Dump bridge is on, click to disable' : 'Dump bridge is off, click to enable'
  );
</script>

<button
  type="button"
  {title}
  {onclick}
  disabled={busy}
  aria-pressed={enabled}
  class="relative w-6 h-6 flex items-center justify-center rounded transition-colors disabled:opacity-40
    {enabled
      ? 'text-emerald-600 dark:text-emerald-400 hover:bg-emerald-50 dark:hover:bg-emerald-900/20'
      : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5'}"
>
  <!-- Antenna tower: triangular mast, two cross-struts, transmitter tip. -->
  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24">
    <path d="M12 4 L6 21" />
    <path d="M12 4 L18 21" />
    <path d="M9 12 L15 12" />
    <path d="M7.5 17 L16.5 17" />
    <circle cx="12" cy="4" r="1.25" fill="currentColor" stroke="none" />
  </svg>

  {#if enabled}
    <span class="absolute top-0.5 right-0.5 flex items-center justify-center w-2 h-2">
      <span class="absolute inline-flex w-full h-full rounded-full bg-emerald-400 opacity-75 lerd-bridge-ping"></span>
      <span class="relative inline-flex w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
    </span>
  {/if}
</button>

<style>
  /* Slower-than-Tailwind ping so the dot reads as ambient activity rather
     than an attention-grabbing alert. Two-second cycle with a soft fade
     keeps it visible without being distracting. */
  .lerd-bridge-ping {
    animation: lerd-bridge-ping 2s cubic-bezier(0, 0, 0.2, 1) infinite;
  }
  @keyframes lerd-bridge-ping {
    0%   { transform: scale(1);   opacity: 0.75; }
    75%, 100% { transform: scale(2); opacity: 0; }
  }
</style>
