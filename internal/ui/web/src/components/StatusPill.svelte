<script lang="ts" module>
  export type PillTone = 'ok' | 'error' | 'warn' | 'muted';
</script>

<script lang="ts">
  interface Props {
    tone: PillTone;
    label: string;
  }
  let { tone, label }: Props = $props();

  const toneClass: Record<PillTone, string> = {
    ok: 'bg-emerald-100 dark:bg-emerald-500/10 text-emerald-700 dark:text-emerald-500',
    error: 'bg-red-100 dark:bg-red-500/10 text-red-600 dark:text-red-400',
    warn: 'bg-yellow-100 dark:bg-yellow-500/10 text-yellow-700 dark:text-yellow-400',
    muted: 'bg-gray-100 dark:bg-white/5 text-gray-600 dark:text-gray-400'
  };

  const dotClass: Record<PillTone, string> = {
    ok: 'bg-emerald-500',
    error: 'bg-red-500',
    warn: 'bg-yellow-500',
    muted: 'bg-gray-400'
  };
</script>

<span
  class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full {toneClass[tone]}"
>
  <span class="w-1.5 h-1.5 rounded-full {dotClass[tone]}"></span>{label}
</span>
