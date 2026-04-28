<script lang="ts">
  import ActionButton from './ActionButton.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    title: string;
    tone?: 'default' | 'accent' | 'success' | 'danger';
    disabled?: boolean;
    loading?: boolean;
    onclick?: (e: MouseEvent) => void;
  }
  let { title, tone, disabled, loading, onclick }: Props = $props();
</script>

<ActionButton {title} {tone} {disabled} {loading} {onclick}>
  <Icon name="refresh" />
</ActionButton>
