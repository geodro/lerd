<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { apiFetch } from '$lib/api';

  // Topic cards point at the GitHub-rendered markdown so the user gets full
  // formatting/links without us bundling a markdown renderer in the binary.
  // Fork branch is hard-coded; will need bumping when 1.22.x lands.
  const DOCS_BASE = 'https://github.com/gabriel-sousa99/lerd/blob/oracle-oci8-support/docs/debug';
  interface Topic {
    id: string;
    title: string;
    teaser: string;
  }
  const topics: Topic[] = [
    { id: 'podman', title: 'Podman', teaser: 'Rootless, quadlets, network lerd, restart cascades' },
    { id: 'nginx', title: 'Nginx', teaser: '502 ao FPM, SSL/mkcert, vhost regen, lan-share' },
    { id: 'dns', title: 'DNS', teaser: '.localhost vs .test, NSS, dnsmasq, IPv6' },
    { id: 'php-fpm', title: 'PHP-FPM', teaser: 'image hash, extensões, php.ini, Xdebug' },
    { id: 'oracle', title: 'Oracle / oci8', teaser: 'ORA-* códigos, Instant Client, NLS_LANG' },
    { id: 'sites', title: 'Sites & link', teaser: '.lerd.yaml, sites.yaml drift, framework detect' },
    { id: 'services', title: 'Services', teaser: 'mysql/postgres/redis quadlets, port conflicts' },
    { id: 'workers', title: 'Workers', teaser: 'queue/horizon/schedule/reverb (systemd user)' },
    { id: 'updates', title: 'Updates', teaser: 'lerd update, versionamento -oracle.N, rollback' }
  ];

  interface ActionResult {
    label: string;
    output: string;
    error?: string;
    ok: boolean;
    durationMs: number;
  }

  let runningAction = $state(''); // action id currently running
  let results = $state<ActionResult[]>([]);
  let copyOk = $state(false);

  async function runAction(action: string, label: string) {
    runningAction = action;
    const started = performance.now();
    try {
      const res = await apiFetch('/api/debug/' + action, { method: 'POST' });
      const json = (await res.json()) as { ok?: boolean; output?: string; error?: string };
      const durationMs = Math.round(performance.now() - started);
      const newResult: ActionResult = {
        label,
        output: json.output ?? '',
        error: json.error,
        ok: Boolean(json.ok),
        durationMs
      };
      // Newest at top, dedupe by label so re-running replaces.
      results = [newResult, ...results.filter((r) => r.label !== label)];
    } catch (e) {
      results = [
        {
          label,
          output: '',
          error: e instanceof Error ? e.message : String(e),
          ok: false,
          durationMs: 0
        },
        ...results.filter((r) => r.label !== label)
      ];
    } finally {
      runningAction = '';
    }
  }

  async function runAllForReport() {
    // Sequential so we keep order in the resulting report.
    await runAction('about', 'lerd about');
    await runAction('doctor', 'lerd doctor');
    await runAction('dns-check', 'lerd dns:check');
    await runAction('containers', 'podman ps -a');
    await runAction('recent-logs', 'journalctl --user lerd-*');
  }

  async function onCopyReport() {
    if (results.length === 0) {
      await runAllForReport();
    }
    const report =
      '# Lerd debug report (gerado pelo dashboard)\n\n' +
      results
        .slice()
        .reverse() // oldest first in the report
        .map(
          (r) =>
            `## ${r.label}  (${r.ok ? 'ok' : 'falha'}, ${r.durationMs}ms)\n` +
            (r.error ? `\nERRO: ${r.error}\n` : '') +
            '\n```\n' +
            r.output +
            '\n```\n'
        )
        .join('\n');
    try {
      await navigator.clipboard.writeText(report);
      copyOk = true;
      setTimeout(() => (copyOk = false), 2000);
    } catch {
      // Clipboard API may be unavailable on http (non-https) — fall back
      // to a textarea-select-copy. Keep simple: just notify.
      alert('Não foi possível copiar automaticamente. Selecione o texto na seção de saída abaixo e copie manualmente.');
    }
  }
</script>

<DetailPanel>
  <div class="px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <h2 class="font-semibold text-gray-900 dark:text-white text-base flex items-center gap-2">
      <span>🛠️</span>
      Debug & Troubleshoot
    </h2>
    <p class="text-xs text-gray-500 dark:text-gray-400 mt-1 leading-relaxed">
      Diagnósticos rápidos + guias por tópico. Use os botões abaixo pra rodar checagens contra a instalação atual; clique nos cards pra abrir os guias completos em pt-BR no GitHub.
    </p>
  </div>

  <!-- Quick actions -->
  <div class="px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">Diagnósticos rápidos</p>
    <div class="flex flex-wrap gap-2">
      <DetailButton
        onclick={() => runAction('doctor', 'lerd doctor')}
        disabled={runningAction !== ''}
        loading={runningAction === 'doctor'}
        title="Bateria de checagens (DNS, podman, certs, services)"
      >lerd doctor</DetailButton>
      <DetailButton
        onclick={() => runAction('dns-check', 'lerd dns:check')}
        disabled={runningAction !== ''}
        loading={runningAction === 'dns-check'}
        title="Diagnóstico em camadas do resolver DNS"
      >lerd dns:check</DetailButton>
      <DetailButton
        onclick={() => runAction('containers', 'podman ps -a')}
        disabled={runningAction !== ''}
        loading={runningAction === 'containers'}
        title="Lista todos os containers (incluindo parados)"
      >podman ps -a</DetailButton>
      <DetailButton
        onclick={() => runAction('recent-logs', 'journalctl --user lerd-*')}
        disabled={runningAction !== ''}
        loading={runningAction === 'recent-logs'}
        title="Últimas 200 linhas de logs dos serviços lerd"
      >últimos logs</DetailButton>
      <DetailButton
        tone="success"
        onclick={onCopyReport}
        disabled={runningAction !== ''}
        title="Roda todos os diagnósticos e copia tudo pra clipboard"
      >{copyOk ? '✓ copiado!' : '📋 Copiar relatório'}</DetailButton>
    </div>
  </div>

  <!-- Topic cards -->
  <div class="px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">Guias por tópico</p>
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
      {#each topics as t (t.id)}
        <a
          href="{DOCS_BASE}/{t.id}.md"
          target="_blank"
          rel="noopener noreferrer"
          class="block px-3 py-2.5 bg-gray-50 dark:bg-white/5 border border-gray-200 dark:border-lerd-border rounded hover:bg-emerald-50 hover:border-emerald-300 dark:hover:bg-emerald-500/10 dark:hover:border-emerald-500/50 transition-colors"
        >
          <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{t.title}</p>
          <p class="text-[11px] text-gray-500 dark:text-gray-400 mt-0.5 leading-tight">{t.teaser}</p>
          <p class="text-[10px] text-emerald-600 dark:text-emerald-400 mt-1.5">ver guia →</p>
        </a>
      {/each}
    </div>
  </div>

  <!-- Output -->
  {#if results.length > 0}
    <div class="px-3 sm:px-5 py-4 space-y-3 overflow-y-auto">
      <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Saída</p>
      {#each results as r (r.label)}
        <div class="bg-gray-50 dark:bg-black/30 border border-gray-200 dark:border-lerd-border rounded">
          <div class="flex items-center justify-between px-3 py-1.5 border-b border-gray-200 dark:border-lerd-border">
            <span class="font-mono text-xs font-medium text-gray-700 dark:text-gray-300">$ {r.label}</span>
            <span class="text-[10px] {r.ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}">
              {r.ok ? '✓' : '✗'} {r.durationMs}ms
            </span>
          </div>
          {#if r.error}
            <p class="px-3 py-1.5 text-[11px] text-red-600 dark:text-red-400 border-b border-red-200 dark:border-red-500/20 bg-red-50 dark:bg-red-500/10">
              {r.error}
            </p>
          {/if}
          <pre class="px-3 py-2 text-[11px] font-mono text-gray-700 dark:text-gray-300 overflow-x-auto whitespace-pre">{r.output || '(sem saída)'}</pre>
        </div>
      {/each}
    </div>
  {/if}
</DetailPanel>
