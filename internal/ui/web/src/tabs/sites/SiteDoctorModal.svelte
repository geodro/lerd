<script lang="ts" module>
  type Status = 'ok' | 'warn' | 'fail' | 'unknown';

  // Per-status visual treatment: the far-right result icon, its colour, the
  // hero count-pill styling and the severity ordering. One table keeps the
  // markup free of fanned-out switch statements.
  const STATUS: Record<Status, { rank: number; icon: string; text: string; badge: string; dot: string }> = {
    fail: {
      rank: 0,
      icon: 'M9 9l6 6m0-6l-6 6m12-3a9 9 0 11-18 0 9 9 0 0118 0z',
      text: 'text-red-500',
      badge: 'bg-red-500/15 text-red-400',
      dot: 'bg-red-500'
    },
    warn: {
      rank: 1,
      icon: 'M12 9v2m0 4h.01M5.07 19h13.86a2 2 0 001.74-3L13.74 4a2 2 0 00-3.48 0L3.33 16a2 2 0 001.74 3z',
      text: 'text-amber-500',
      badge: 'bg-amber-500/15 text-amber-500',
      dot: 'bg-amber-500'
    },
    unknown: {
      rank: 2,
      icon: 'M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
      text: 'text-gray-400',
      badge: 'bg-gray-500/15 text-gray-400',
      dot: 'bg-gray-400'
    },
    ok: {
      rank: 3,
      icon: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z',
      text: 'text-emerald-500',
      badge: 'bg-emerald-500/15 text-emerald-500',
      dot: 'bg-emerald-500'
    }
  };

  // Per-check glyph for the neutral leading tile, so each row is recognisable
  // at a glance regardless of its result. Falls back to a clipboard.
  const CHECK_ICON: Record<string, string> = {
    app_key:
      'M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z',
    env_drift:
      'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
    app_debug:
      'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4',
    storage_link:
      'M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1',
    migrations:
      'M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4',
    env_present:
      'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
    composer_deps:
      'M21 7.5l-9-5.25L3 7.5m18 0l-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9',
    node_deps:
      'M21 7.5l-9-5.25L3 7.5m18 0l-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9',
    composer_audit:
      'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z',
    node_audit:
      'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z',
    php_version:
      'M9 3.75H6.912a2.25 2.25 0 00-2.15 1.588L2.35 13.177a2.25 2.25 0 00-.1.661V18a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18v-4.162c0-.224-.034-.447-.1-.661L19.24 5.338a2.25 2.25 0 00-2.15-1.588H15M2.25 13.5h3.86a2.25 2.25 0 012.012 1.244l.256.512a2.25 2.25 0 002.013 1.244h3.218a2.25 2.25 0 002.013-1.244l.256-.512a2.25 2.25 0 012.013-1.244h3.859M12 3v8.25m0 0l-3-3m3 3l3-3'
  };
  const FALLBACK_ICON = 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2';
</script>

<script lang="ts">
  import Modal from '$components/Modal.svelte';
  import { loadDoctor, type DoctorCheck, type DoctorReport } from '$stores/doctor';
  import { loadCommands, executeCommand, executeDoctorFix, type Command } from '$stores/commands';
  import { goToTab } from '$stores/route';
  import type { Site } from '$stores/sites';
  import { m } from '../../paraglide/messages.js';

  interface Props {
    open: boolean;
    site: Site;
    branch?: string;
    onclose: () => void;
  }
  let { open, site, branch = '', onclose }: Props = $props();

  let report = $state<DoctorReport | null>(null);
  let commands = $state<Command[]>([]);
  let loading = $state(false);
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

  // Run the checks only when the modal is opened, so the migrate:status exec
  // fires on an explicit click rather than eagerly on every site view. Stale
  // results from a prior site/branch are cleared so the spinner shows instead
  // of last run's findings while the fresh report loads.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      report = null;
      reload();
    }
    wasOpen = open;
  });

  // Universal package-manager fixes run through the doctor fix endpoint rather
  // than a framework command. The key matches sitedoctor's fix constants.
  const DOCTOR_FIX: Record<string, string> = {
    composer_install: 'composer install',
    composer_update: 'composer update',
    npm_install: 'npm install',
    npm_audit_fix: 'npm audit fix'
  };

  async function runFix(check: DoctorCheck) {
    if (!check.fix || fixing) return;
    fixing = check.name;
    try {
      // Both paths drive the global CommandRunModal so the user sees streamed
      // output; once the run finishes we re-check.
      if (check.fix in DOCTOR_FIX) {
        await executeDoctorFix(site.domain, check.fix, 'Run ' + DOCTOR_FIX[check.fix], branch);
      } else {
        const cmd = commands.find((c) => c.name === check.fix);
        if (!cmd) return;
        await executeCommand(site.domain, cmd, branch);
      }
      await reload();
    } finally {
      fixing = '';
    }
  }

  const st = (s: DoctorCheck['status']) => STATUS[(s as Status) in STATUS ? (s as Status) : 'unknown'];

  const checkTitle = (check: DoctorCheck): string => {
    switch (check.name) {
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
        return check.label ?? check.name;
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

  // Surface problems first: fail, then warn, then unknown, then healthy. Stable
  // within a tier so the backend's meaningful order is preserved.
  const sorted = $derived(
    report ? [...report.checks].sort((a, b) => st(a.status).rank - st(b.status).rank) : []
  );

  const allClear = $derived(Boolean(report) && report!.failures === 0 && report!.warnings === 0);
  const okCount = $derived(report ? report.checks.filter((c) => c.status === 'ok').length : 0);

  const canFix = (check: DoctorCheck): boolean =>
    Boolean(check.fix) && (check.fix! in DOCTOR_FIX || commands.some((c) => c.name === check.fix));

  // Env drift has no automated fix; offer a pencil that jumps to the Env tab so
  // the user can reconcile .env by hand. Only when there's actually a warning.
  const canEditEnv = (check: DoctorCheck): boolean =>
    check.name === 'env_drift' && check.status !== 'ok';

  function openEnv() {
    onclose();
    goToTab('sites', `${site.domain}/env`);
  }
</script>

<Modal {open} title={m.sites_doctor_title()} size="lg" {onclose}>
  <div class="max-h-[64vh] overflow-y-auto px-5 py-4 space-y-4">
    <!-- Summary hero: overall verdict plus counts, so the headline reads before any scrolling. -->
    <div
      class="flex items-center gap-3 rounded-xl border p-3.5 {allClear
        ? 'border-emerald-500/20 bg-emerald-500/5'
        : (report?.failures ?? 0) > 0
          ? 'border-red-500/20 bg-red-500/5'
          : 'border-amber-500/20 bg-amber-500/5'}"
    >
      <span
        class="grid place-items-center w-10 h-10 rounded-full shrink-0 {allClear
          ? 'bg-emerald-500/15 text-emerald-500'
          : (report?.failures ?? 0) > 0
            ? 'bg-red-500/15 text-red-500'
            : 'bg-amber-500/15 text-amber-500'}"
      >
        {#if loading && !report}
          <svg class="w-5 h-5 animate-spin" viewBox="0 0 24 24" fill="none">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" />
            <path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.4 0 0 5.4 0 12h4z" />
          </svg>
        {:else}
          <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d={allClear ? STATUS.ok.icon : (report?.failures ?? 0) > 0 ? STATUS.fail.icon : STATUS.warn.icon}
            />
          </svg>
        {/if}
      </span>
      <div class="min-w-0 flex-1">
        <p class="text-sm font-semibold text-gray-900 dark:text-white">
          {loading && !report ? m.sites_doctor_running() : allClear ? m.sites_doctor_allClear() : m.sites_doctor_summary({ failures: report?.failures ?? 0, warnings: report?.warnings ?? 0 })}
        </p>
        <p class="text-[11px] text-gray-500 dark:text-gray-400 truncate">{site.domain}</p>
      </div>
      {#if report}
        <div class="hidden sm:flex items-center gap-1.5 shrink-0">
          {#if report.failures > 0}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold {STATUS.fail.badge}">
              <span class="w-1.5 h-1.5 rounded-full {STATUS.fail.dot}"></span>{report.failures}
            </span>
          {/if}
          {#if report.warnings > 0}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold {STATUS.warn.badge}">
              <span class="w-1.5 h-1.5 rounded-full {STATUS.warn.dot}"></span>{report.warnings}
            </span>
          {/if}
          {#if okCount > 0}
            <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold {STATUS.ok.badge}">
              <span class="w-1.5 h-1.5 rounded-full {STATUS.ok.dot}"></span>{okCount}
            </span>
          {/if}
        </div>
      {/if}
    </div>

    {#if error}
      <div class="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-xs text-red-400">
        <svg class="w-4 h-4 mt-0.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d={STATUS.fail.icon} />
        </svg>
        <span>{error}</span>
      </div>
    {:else if loading && !report}
      <!-- Skeleton rows while the first run (incl. migrate:status) is in flight. -->
      <div class="rounded-xl border border-gray-200/80 dark:border-lerd-border divide-y divide-gray-100 dark:divide-lerd-border overflow-hidden">
        {#each Array(5) as _, i (i)}
          <div class="flex items-center gap-3 px-3.5 py-2.5 animate-pulse">
            <span class="w-8 h-8 rounded-lg bg-gray-200 dark:bg-white/5 shrink-0"></span>
            <div class="flex-1 space-y-1.5">
              <div class="h-3 w-32 rounded bg-gray-200 dark:bg-white/5"></div>
              <div class="h-2.5 w-48 rounded bg-gray-100 dark:bg-white/[0.03]"></div>
            </div>
            <span class="w-5 h-5 rounded-full bg-gray-200 dark:bg-white/5 shrink-0"></span>
          </div>
        {/each}
      </div>
    {:else if report && report.checks.length === 0}
      <div class="flex flex-col items-center justify-center gap-2 py-10 text-center">
        <svg class="w-8 h-8 text-gray-300 dark:text-gray-600" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path stroke-linecap="round" stroke-linejoin="round" d={FALLBACK_ICON} />
        </svg>
        <p class="text-xs text-gray-500 dark:text-gray-400">{m.sites_doctor_empty()}</p>
      </div>
    {:else if report}
      <!-- Compact stacked list: one bordered container, rows split by dividers. -->
      <div class="rounded-xl border border-gray-200/80 dark:border-lerd-border divide-y divide-gray-100 dark:divide-lerd-border overflow-hidden">
        {#each sorted as check (check.name)}
          <div
            class="flex items-center gap-3 px-3.5 py-2.5 transition-colors {check.status === 'fail'
              ? 'bg-red-500/[0.04]'
              : check.status === 'warn'
                ? 'bg-amber-500/[0.03]'
                : ''}"
          >
            <!-- Neutral leading glyph identifies the check; colour lives in the result icon on the right. -->
            <span class="grid place-items-center w-8 h-8 rounded-lg shrink-0 bg-gray-100 dark:bg-white/5 text-gray-500 dark:text-gray-400">
              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d={CHECK_ICON[check.name] ?? FALLBACK_ICON} />
              </svg>
            </span>
            <div class="min-w-0 flex-1">
              <span class="text-sm font-medium text-gray-900 dark:text-white">{checkTitle(check)}</span>
              {#if check.detail}
                <p class="mt-0.5 text-[11px] leading-snug text-gray-500 dark:text-gray-400 break-words">{check.detail}</p>
              {/if}
            </div>
            {#if canFix(check)}
              <button
                type="button"
                onclick={() => runFix(check)}
                disabled={Boolean(fixing)}
                class="shrink-0 inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-semibold text-white shadow-sm disabled:opacity-50 transition-colors {check.status === 'fail' ? 'bg-lerd-red hover:bg-lerd-redhov' : 'bg-amber-500 hover:bg-amber-600'}"
              >
                {#if fixing === check.name}
                  <svg class="w-3.5 h-3.5 animate-spin" viewBox="0 0 24 24" fill="none">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" />
                    <path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.4 0 0 5.4 0 12h4z" />
                  </svg>
                  {m.sites_doctor_fixing()}
                {:else}
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
                  </svg>
                  {m.sites_doctor_fix()}
                {/if}
              </button>
            {/if}
            {#if canEditEnv(check)}
              <button
                type="button"
                onclick={openEnv}
                class="shrink-0 inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:border-lerd-red hover:text-lerd-red transition-colors"
              >
                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                </svg>
                {m.common_edit()}
              </button>
            {/if}
            <!-- Result on the far right, as a check / warning / error / unknown icon. -->
            <span class="shrink-0 {st(check.status).text}" title={statusLabel(check.status)} aria-label={statusLabel(check.status)}>
              <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d={st(check.status).icon} />
              </svg>
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    <button
      type="button"
      onclick={reload}
      disabled={loading || Boolean(fixing)}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border border-gray-200 dark:border-lerd-border text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-white/5 disabled:opacity-50 transition-colors"
    >
      <svg class="w-3.5 h-3.5 {loading ? 'animate-spin' : ''}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
      </svg>
      {loading ? m.sites_doctor_running() : m.sites_doctor_refresh()}
    </button>
  {/snippet}
</Modal>
