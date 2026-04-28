<script lang="ts">
  import Modal from './Modal.svelte';

  interface Props {
    open: boolean;
    title: string;
    onclose: () => void;
    withFooter?: boolean;
  }
  let { open, title, onclose, withFooter = false }: Props = $props();
</script>

{#if withFooter}
  {#snippet footer()}<span data-testid="footer">F</span>{/snippet}
  <Modal {open} {title} {onclose} {footer}>
    <div data-testid="body">BODY</div>
  </Modal>
{:else}
  <Modal {open} {title} {onclose}>
    <div data-testid="body">BODY</div>
  </Modal>
{/if}
