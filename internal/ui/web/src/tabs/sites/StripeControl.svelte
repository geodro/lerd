<script lang="ts">
  import ToggleButton from '$components/ToggleButton.svelte';
  import StripeConfigModal from './StripeConfigModal.svelte';
  import { m } from '../../paraglide/messages.js';

  // Stripe listener on/off plus a gear that edits the forward webhook path,
  // rendered as one segmented control mirroring HorizonControl. The gear is
  // always available (even while stopped) so the route can be set before the
  // listener is first enabled, which is what non-Laravel projects need.
  interface Props {
    running: boolean;
    asleep?: boolean;
    loading?: boolean;
    webhookPath?: string;
    onToggle: () => void;
    onSaveConfig: (path: string) => void;
  }
  let { running, asleep = false, loading = false, webhookPath = '', onToggle, onSaveConfig }: Props = $props();

  let modalOpen = $state(false);
</script>

<div class="inline-flex items-center">
  <ToggleButton
    label={m.sites_controls_stripe()}
    on={running}
    {asleep}
    {loading}
    disabled={loading}
    rounding="rounded-l-md border-r-0"
    title={running ? m.sites_controls_stripeToggle_on() : m.sites_controls_stripeToggle_off()}
    onclick={onToggle}
  />
  <button
    type="button"
    title={m.sites_controls_stripeConfig()}
    aria-label={m.sites_controls_stripeConfig()}
    onclick={() => (modalOpen = true)}
    class="inline-flex items-center justify-center h-7 w-8 rounded-r-md border border-gray-200 dark:border-lerd-border bg-white dark:bg-lerd-card hover:bg-gray-50 dark:hover:bg-white/5 transition-colors text-gray-400 dark:text-gray-500"
  >
    <svg
      class="w-3.5 h-3.5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <circle cx="12" cy="12" r="3" />
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
      />
    </svg>
  </button>
</div>

<StripeConfigModal
  open={modalOpen}
  path={webhookPath}
  onclose={() => (modalOpen = false)}
  onsave={onSaveConfig}
/>
