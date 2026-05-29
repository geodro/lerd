<script lang="ts">
  import Modal from './Modal.svelte';

  interface Props {
    open: boolean;
    title: string;
    onclose: () => void;
    withFooter?: boolean;
    size?: 'sm' | 'md' | 'lg' | 'xl';
  }
  let { open, title, onclose, withFooter = false, size = 'md' }: Props = $props();
</script>

{#if withFooter}
  {#snippet footer()}<span data-testid="footer">F</span>{/snippet}
  <Modal {open} {title} {onclose} {size} {footer}>
    <div data-testid="body">BODY</div>
  </Modal>
{:else}
  <Modal {open} {title} {onclose} {size}>
    <div data-testid="body">BODY</div>
  </Modal>
{/if}
