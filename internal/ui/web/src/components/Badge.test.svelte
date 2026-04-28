<script lang="ts">
  import Badge, { type BadgeTone } from './Badge.svelte';

  interface Props {
    tone: BadgeTone;
    dot?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    label: string;
  }
  let { tone, dot, title, onclick, label }: Props = $props();
</script>

<Badge {tone} {dot} {title} {onclick}>{label}</Badge>
