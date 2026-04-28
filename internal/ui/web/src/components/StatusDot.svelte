<script lang="ts" module>
  export type StatusColor = 'green' | 'red' | 'yellow' | 'gray' | 'blue' | 'amber' | 'violet' | 'sky' | 'indigo' | 'emerald';
</script>

<script lang="ts">
  interface Props {
    color: StatusColor;
    size?: 'xs' | 'sm' | 'md';
    pulse?: boolean;
  }
  let { color, size = 'sm', pulse = false }: Props = $props();

  const colorClass: Record<StatusColor, string> = {
    green: 'bg-emerald-500',
    emerald: 'bg-emerald-500',
    red: 'bg-red-500',
    yellow: 'bg-yellow-400',
    gray: 'bg-gray-300 dark:bg-gray-600',
    blue: 'bg-blue-500',
    amber: 'bg-amber-400',
    violet: 'bg-violet-400',
    sky: 'bg-sky-400',
    indigo: 'bg-indigo-400'
  };

  const sizeClass: Record<NonNullable<Props['size']>, string> = {
    xs: 'w-1.5 h-1.5',
    sm: 'w-2 h-2',
    md: 'w-2.5 h-2.5'
  };

  const dotClass = $derived(`${sizeClass[size]} ${colorClass[color]} rounded-full shrink-0${pulse ? ' animate-pulse' : ''}`);
</script>

<span class={dotClass}></span>
