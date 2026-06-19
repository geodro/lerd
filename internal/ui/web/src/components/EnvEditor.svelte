<script lang="ts">
  import MonacoEditor from './MonacoEditor.svelte';

  interface Props {
    value: string;
    readOnly?: boolean;
    onChange?: (next: string) => void;
  }
  let { value = $bindable(''), readOnly = false, onChange }: Props = $props();
</script>

<MonacoEditor bind:value {readOnly} {onChange} language="dotenv" />
