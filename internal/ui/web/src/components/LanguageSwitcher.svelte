<script lang="ts">
  import { locale, changeLocale, LOCALES, LOCALE_LABELS, LOCALE_CODES, type Locale } from '$stores/locale';

  interface Props {
    variant?: 'compact' | 'select';
  }
  let { variant = 'select' }: Props = $props();

  function onChange(e: Event) {
    const v = (e.target as HTMLSelectElement).value as Locale;
    if (v !== $locale) changeLocale(v);
  }
</script>

{#if variant === 'compact'}
  <div class="flex items-center rounded-md border border-gray-200 dark:border-lerd-border overflow-hidden">
    {#each LOCALES as l (l)}
      <button
        title={LOCALE_LABELS[l]}
        onclick={() => changeLocale(l)}
        class="px-2 py-1.5 text-[10px] font-medium transition-colors {$locale === l
          ? 'bg-gray-200 dark:bg-white/10 text-gray-900 dark:text-white'
          : 'text-gray-400 dark:text-gray-500 hover:bg-gray-100 dark:hover:bg-white/5'}"
      >{LOCALE_CODES[l]}</button>
    {/each}
  </div>
{:else}
  <select
    value={$locale}
    onchange={onChange}
    class="text-xs bg-white dark:bg-lerd-muted border border-gray-200 dark:border-lerd-border rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-gray-300 dark:hover:border-lerd-muted focus:outline-none focus:border-lerd-red/50 cursor-pointer transition-colors"
  >
    {#each LOCALES as l (l)}
      <option value={l}>{LOCALE_LABELS[l]}</option>
    {/each}
  </select>
{/if}
