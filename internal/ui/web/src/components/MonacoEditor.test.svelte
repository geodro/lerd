<script lang="ts">
  import MonacoEditor from './MonacoEditor.svelte';
  import type * as Monaco from 'monaco-editor';

  interface Props {
    value?: string;
    language?: string;
    readOnly?: boolean;
    onChange?: (next: string) => void;
    onReady?: (ctx: { editor: Monaco.editor.IStandaloneCodeEditor; monaco: typeof Monaco }) => void;
  }
  let { value = $bindable(''), language, readOnly, onChange, onReady }: Props = $props();
</script>

<MonacoEditor bind:value {language} {readOnly} {onChange} {onReady} />
