<script lang="ts">
  import ButtonMenu, { type ButtonMenuAction } from './ButtonMenu.svelte';

  interface Props {
    actions: ButtonMenuAction[];
    busy?: boolean;
    onSettings?: () => void;
    settingsTitle?: string;
  }
  let { actions, busy = false, onSettings, settingsTitle }: Props = $props();
</script>

<ButtonMenu {actions} {busy} {onSettings} {settingsTitle} />
