<script lang="ts">
  import StatusDot from '$components/StatusDot.svelte';
  import type { StatusColor } from '$components/StatusDot.svelte';
  import { loadDoctor, type DoctorCheck, type DoctorReport } from '$stores/doctor';
  import { loadCommands, executeCommand, type Command } from '$stores/commands';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    site: Site;
    branch: string;
  }
  let { site, branch }: Props = $props();

  let report = $state<DoctorReport | null>(null);
  let commands = $state<Command[]>([]);
  let loading = $state(true);
  let error = $state('');
  // Name of the check whose fix command is currently running, so only its
  // button shows a spinner and the rest stay disabled (one run at a time).
  let fixing = $state('');

  async function reload() {
    loading = true;
    error = '';
    const domain = site.domain;
    const b = branch;
    try {
      const [r, cmds] = await Promise.all([loadDoctor(domain, b), loadCommands(domain, b)]);
      if (site.domain !== domain || branch !== b) return;
      report = r;
      commands = cmds;
    } catch (e) {
      if (site.domain === domain && branch === b) error = e instanceof Error ? e.message : 'Failed to load';
    } finally {
      if (site.domain === domain && branch === b) loading = false;
    }
  }

  // Run checks whenever the site or branch changes.
  $effect(() => {
    void site.domain;
    void branch;
    reload();
  });

  async function runFix(check: DoctorCheck) {
    if (!check.fix || fixing) return;
    const cmd = commands.find((c) => c.name === check.fix);
    if (!cmd) return;
    fixing = check.name;
    try {
      // executeCommand drives the global CommandRunModal so the user sees the
      // command's output; it resolves once the run finishes, then we re-check.
      await executeCommand(site.domain, cmd, branch);
      await reload();
    } finally {
      fixing = '';
    }
  }

  const dotColor = (s: DoctorCheck['status']): StatusColor =>
    s === 'fail' ? 'red' : s === 'warn' ? 'amber' : s === 'ok' ? 'green' : 'gray';

  const checkTitle = (name: string): string => {
    switch (name) {
      case 'app_key':
        return m.sites_doctor_check_appKey();
      case 'env_drift':
        return m.sites_doctor_check_envDrift();
      case 'app_debug':
        return m.sites_doctor_check_appDebug();
      case 'storage_link':
        return m.sites_doctor_check_storageLink();
      case 'migrations':
        return m.sites_doctor_check_migrations();
      default:
        return name;
    }
  };

  const statusLabel = (s: DoctorCheck['status']): string => {
    switch (s) {
      case 'ok':
        return m.sites_doctor_status_ok();
      case 'warn':
        return m.sites_doctor_status_warn();
      case 'fail':
        return m.sites_doctor_status_fail();
      default:
        return m.sites_doctor_status_unknown();
    }
  };

  const statusTextClass = (s: DoctorCheck['status']): string => {
    switch (s) {
      case 'fail':
        return 'text-red-500';
      case 'warn':
        return 'text-amber-500';
      case 'ok':
        return 'text-emerald-500';
      default:
        return 'text-gray-400';
    }
  };

  const summary = $derived(
    report
      ? report.failures === 0 && report.warnings === 0
        ? m.sites_doctor_allClear()
        : m.sites_doctor_summary({ failures: report.failures, warnings: report.warnings })
      : ''
  );

  const canFix = (check: DoctorCheck): boolean =>
    Boolean(check.fix) && commands.some((c) => c.name === check.fix);
</script>

<div class="flex-1 overflow-y-auto p-4 space-y-3">
  <div class="flex items-center justify-between gap-3">
    <p class="text-xs text-gray-500 dark:text-gray-400 truncate">{summary}</p>
    <button
      type="button"
      onclick={reload}
      disabled={loading || Boolean(fixing)}
      class="shrink-0 inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
    >
      {loading ? m.sites_doctor_running() : m.sites_doctor_refresh()}
    </button>
  </div>

  {#if error}
    <p class="text-xs text-red-500">{error}</p>
  {:else if loading && !report}
    <p class="text-xs text-gray-500 dark:text-gray-400">{m.sites_doctor_running()}</p>
  {:else if report && report.checks.length === 0}
    <p class="text-xs text-gray-500 dark:text-gray-400">{m.sites_doctor_empty()}</p>
  {:else if report}
    {#each report.checks as check (check.name)}
      <div
        class="flex items-start gap-3 rounded-lg border border-gray-200/80 dark:border-lerd-border bg-white dark:bg-lerd-card p-3"
      >
        <span class="mt-1.5"><StatusDot color={dotColor(check.status)} /></span>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-900 dark:text-white">{checkTitle(check.name)}</span>
            <span class="text-[10px] font-semibold uppercase tracking-wide {statusTextClass(check.status)}">
              {statusLabel(check.status)}
            </span>
          </div>
          {#if check.detail}
            <p class="mt-0.5 text-[11px] leading-snug text-gray-500 dark:text-gray-400">{check.detail}</p>
          {/if}
        </div>
        {#if canFix(check)}
          <button
            type="button"
            onclick={() => runFix(check)}
            disabled={Boolean(fixing)}
            class="shrink-0 inline-flex items-center px-2.5 py-1 rounded-md text-xs font-medium bg-lerd-red hover:bg-lerd-redhov text-white disabled:opacity-50 transition-colors"
          >
            {fixing === check.name ? m.sites_doctor_fixing() : m.sites_doctor_fix()}
          </button>
        {/if}
      </div>
    {/each}
  {/if}
</div>
