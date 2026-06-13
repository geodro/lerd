<script lang="ts">
  import ToggleButton from '$components/ToggleButton.svelte';
  import { m } from '../../paraglide/messages.js';

  // Horizon on/off and its watch-files (horizon:listen) reload toggle, rendered
  // as one segmented control. The left segment is a ToggleButton (joined with a
  // left-rounded, no-right-border rounding); the reload segment only appears
  // while Horizon is running (or mid-toggle), since reload is a property of a
  // live worker. When reload is on, the refresh icon lights emerald and glows.
  interface Props {
    running: boolean;
    failing?: boolean;
    asleep?: boolean;
    reload?: boolean;
    horizonLoading?: boolean;
    reloadLoading?: boolean;
    onToggle: () => void;
    onToggleReload: () => void;
  }
  let {
    running,
    failing = false,
    asleep = false,
    reload = false,
    horizonLoading = false,
    reloadLoading = false,
    onToggle,
    onToggleReload
  }: Props = $props();

  const showReload = $derived(running || reloadLoading);

  // While a reload toggle is in flight the desired state is in flux, so show the
  // icon neutral (not a misleading green glow that lags the user's intent),
  // settling to the real state once the restart completes.
  const iconClass = $derived(
    reloadLoading
      ? 'text-gray-400 dark:text-gray-500'
      : reload
        ? 'text-emerald-500 drop-shadow-[0_0_4px_rgba(16,185,129,0.9)]'
        : 'text-gray-400 dark:text-gray-500'
  );

  // One full turn of the refresh icon as click feedback. Reset on animation end,
  // with a timeout fallback in case animationend never fires (e.g. reduced
  // motion, or the element being hidden), so a later click can spin it again.
  let spinning = $state(false);
  let spinTimer: ReturnType<typeof setTimeout>;
  function clickReload() {
    spinning = true;
    clearTimeout(spinTimer);
    spinTimer = setTimeout(() => (spinning = false), 800);
    onToggleReload();
  }
</script>

<div class="inline-flex items-center">
  <ToggleButton
    label={m.sites_controls_horizon()}
    on={running}
    {failing}
    {asleep}
    loading={horizonLoading}
    disabled={horizonLoading}
    rounding={showReload ? 'rounded-l-md border-r-0' : 'rounded-md'}
    title={failing
      ? m.sites_controls_horizonToggle_failing()
      : running
        ? m.sites_controls_horizonToggle_on()
        : m.sites_controls_horizonToggle_off()}
    onclick={onToggle}
  />

  {#if showReload}
    <button
      type="button"
      disabled={reloadLoading}
      aria-pressed={reload}
      aria-busy={reloadLoading}
      title={reload
        ? m.sites_controls_horizonReloadToggle_on()
        : m.sites_controls_horizonReloadToggle_off()}
      onclick={clickReload}
      class="inline-flex items-center justify-center h-7 w-8 rounded-r-md border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card hover:bg-gray-50 dark:hover:bg-white/5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
    >
      <svg
        class="w-3.5 h-3.5 transition-colors {iconClass} {spinning ? 'animate-[spin_0.6s_ease-in-out]' : ''}"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        onanimationend={() => (spinning = false)}
      >
        <polyline points="23 4 23 10 17 10" />
        <polyline points="1 20 1 14 7 14" />
        <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
      </svg>
    </button>
  {/if}
</div>
