<script lang="ts">
  import DetailButton, { type DetailButtonTone } from './DetailButton.svelte';

  interface Props {
    tone?: DetailButtonTone;
    loading?: boolean;
    disabled?: boolean;
    href?: string;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    label: string;
  }
  let { tone, loading, disabled, href, title, onclick, label }: Props = $props();
</script>

<DetailButton {tone} {loading} {disabled} {href} {title} {onclick}>{label}</DetailButton>
