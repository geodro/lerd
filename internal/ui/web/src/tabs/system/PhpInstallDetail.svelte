<script lang="ts">
  import DetailPanel from '$components/DetailPanel.svelte';
  import DetailButton from '$components/DetailButton.svelte';
  import { apiFetch } from '$lib/api';
  import { phpVersions, loadPhpVersions } from '$stores/phpVersions';
  import { goToTab } from '$stores/route';

  // Lerd supports 7.4 → 8.5 (the 7.x track is "legacy tier"). 7.0/7.1/7.3
  // exist as base images on docker hub but aren't tracked by lerd's xdebug
  // pinning, so they're not offered here. Add a new entry if/when lerd
  // promotes them.
  interface AvailableVersion {
    version: string;
    label: string;
    note: string;
  }
  const ALL_VERSIONS: AvailableVersion[] = [
    { version: '7.4', label: 'PHP 7.4', note: 'Legacy tier — para apps antigos (oci8 2.2.0, xdebug 3.1).' },
    { version: '8.0', label: 'PHP 8.0', note: 'Legacy tier (oci8 3.0.1, xdebug 3.3).' },
    { version: '8.1', label: 'PHP 8.1', note: 'Estável (oci8 3.3.0).' },
    { version: '8.2', label: 'PHP 8.2', note: 'Estável (oci8 3.3.0).' },
    { version: '8.3', label: 'PHP 8.3', note: 'Atual (oci8 3.3.0). Recomendada para Laravel 11.' },
    { version: '8.4', label: 'PHP 8.4', note: 'Latest stable (oci8 3.4.1). Recomendada para Laravel 13.' },
    { version: '8.5', label: 'PHP 8.5', note: 'Bleeding edge (oci8 3.4.1).' }
  ];

  // Filter out versions that are already installed so the list only shows
  // what the user can act on.
  const missing = $derived(
    ALL_VERSIONS.filter((v) => !$phpVersions.includes(v.version))
  );

  let installing = $state(''); // version being installed (empty when idle)
  let installError = $state('');
  let installSuccess = $state('');

  async function onInstall(version: string) {
    installing = version;
    installError = '';
    installSuccess = '';
    try {
      const res = await apiFetch(
        '/api/php-versions/' + encodeURIComponent(version) + '/install',
        { method: 'POST' }
      );
      const json = (await res.json()) as { ok?: boolean; error?: string };
      if (!json.ok) {
        installError = json.error || `Falha ao instalar PHP ${version}`;
      } else {
        installSuccess = `PHP ${version} instalado e iniciado.`;
        await loadPhpVersions();
        // Auto-navigate to the freshly installed version after a beat
        setTimeout(() => goToTab('system', 'php-' + version), 600);
      }
    } catch (e) {
      installError = e instanceof Error ? e.message : String(e);
    } finally {
      installing = '';
    }
  }
</script>

<DetailPanel>
  <div class="px-3 sm:px-5 py-4 border-b border-gray-100 dark:border-lerd-border">
    <h2 class="font-semibold text-gray-900 dark:text-white text-base">Instalar versão PHP</h2>
    <p class="text-xs text-gray-400 mt-1 leading-relaxed">
      Provisiona uma nova versão PHP-FPM: faz build (ou pull) da imagem base,
      escreve o quadlet systemd e inicia o container. Pode levar 1–3 minutos
      por versão na primeira vez. Equivalente a <code class="font-mono text-gray-500">lerd php:install &lt;versão&gt;</code>.
    </p>
  </div>

  <div class="px-3 sm:px-5 py-4 space-y-2">
    {#if missing.length === 0}
      <p class="text-sm text-gray-400 italic">Todas as versões suportadas já estão instaladas.</p>
    {:else}
      {#each missing as v (v.version)}
        <div class="flex items-center justify-between gap-3 px-3 py-2.5 bg-gray-50 dark:bg-white/5 border border-gray-200 dark:border-lerd-border rounded">
          <div class="min-w-0">
            <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{v.label}</p>
            <p class="text-[11px] text-gray-400 mt-0.5">{v.note}</p>
          </div>
          <DetailButton
            tone="success"
            onclick={() => onInstall(v.version)}
            disabled={installing !== ''}
            loading={installing === v.version}
            title={`Build + quadlet + start para PHP ${v.version}`}
          >{installing === v.version ? 'Instalando…' : 'Instalar'}</DetailButton>
        </div>
      {/each}
    {/if}

    {#if installing}
      <div class="text-xs text-emerald-600 dark:text-emerald-400 leading-relaxed flex items-center gap-1.5 pt-2">
        <span class="inline-block w-1.5 h-1.5 bg-emerald-500 rounded-full animate-pulse"></span>
        Construindo <span class="font-mono">lerd-php{installing.replace('.', '')}-fpm:local</span> e iniciando o container — 1 a 3 minutos…
      </div>
    {/if}

    {#if installError}
      <div class="text-xs font-medium text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-500/10 rounded-lg px-3 py-2 break-words">
        {installError}
      </div>
    {/if}

    {#if installSuccess}
      <div class="text-xs font-medium text-emerald-700 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10 rounded-lg px-3 py-2">
        {installSuccess}
      </div>
    {/if}
  </div>
</DetailPanel>
