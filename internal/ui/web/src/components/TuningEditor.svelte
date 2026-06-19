<script lang="ts">
  import MonacoEditor from './MonacoEditor.svelte';

  interface Props {
    value: string;
    readOnly?: boolean;
    onChange?: (next: string) => void;
  }
  let { value = $bindable(''), readOnly = false, onChange }: Props = $props();
</script>

<!-- Covers ini-style my.cnf / php.ini and redis `directive arg` configs. -->
<MonacoEditor bind:value {readOnly} {onChange} language="ini" />
