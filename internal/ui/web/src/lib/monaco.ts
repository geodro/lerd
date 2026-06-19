// Lazy, single-instance Monaco loader. Monaco is ~5MB, so we keep it out
// of the entry bundle and only pull it (plus its editor web worker) the
// first time an editor surface mounts. Every caller awaits the same
// promise, so the module, the worker wiring, and the themes are set up
// exactly once.
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
import { configLanguages } from '$lib/monaco-langs';

// We deliberately load the bare editor API rather than the `monaco-editor`
// barrel: the barrel registers every bundled language plus the ts/json/
// html/css language services (each with its own worker), none of which we
// use. Pulling editor.api and contributing only the grammars we need keeps
// the embedded dist (and therefore the lerd binary) lean.
export type MonacoModule = typeof import('monaco-editor/esm/vs/editor/editor.api');

let monacoPromise: Promise<MonacoModule> | null = null;

// We only need the core editor worker. PHP and the config formats get
// their intelligence from the LSP bridge (tinker) or lightweight Monarch
// grammars, so the ts/json/html/css language workers are dead weight.
function configureWorkers() {
  self.MonacoEnvironment = {
    getWorker() {
      return new EditorWorker();
    }
  };
}

// Token colours for the config-format Monarch grammars. The `cfg*` token
// names are deliberately distinct from the standard PHP tokens (comment,
// string, keyword, …) so these rules style nginx/ini/dotenv without
// recolouring the tinker PHP editor, which keeps the base theme palette.
const CONFIG_RULES_LIGHT = [
  { token: 'cfgComment', foreground: '9ca3af', fontStyle: 'italic' },
  { token: 'cfgString', foreground: '047857' },
  { token: 'cfgNumber', foreground: 'b45309' },
  { token: 'cfgKey', foreground: '1d4ed8', fontStyle: 'bold' },
  { token: 'cfgBlock', foreground: '7c3aed', fontStyle: 'bold' },
  { token: 'cfgSection', foreground: '7c3aed', fontStyle: 'bold' },
  { token: 'cfgOp', foreground: '9ca3af' },
  { token: 'cfgVar', foreground: 'b45309' },
  { token: 'cfgValue', foreground: '374151' }
];
const CONFIG_RULES_DARK = [
  { token: 'cfgComment', foreground: '9ca3af', fontStyle: 'italic' },
  { token: 'cfgString', foreground: '6ee7b7' },
  { token: 'cfgNumber', foreground: 'fcd34d' },
  { token: 'cfgKey', foreground: '93c5fd', fontStyle: 'bold' },
  { token: 'cfgBlock', foreground: 'c4b5fd', fontStyle: 'bold' },
  { token: 'cfgSection', foreground: 'c4b5fd', fontStyle: 'bold' },
  { token: 'cfgOp', foreground: '9ca3af' },
  { token: 'cfgVar', foreground: 'fcd34d' },
  { token: 'cfgValue', foreground: 'e5e7eb' }
];

function defineThemes(monaco: MonacoModule) {
  monaco.editor.defineTheme('lerd-light', {
    base: 'vs',
    inherit: true,
    rules: CONFIG_RULES_LIGHT,
    colors: {
      'editor.background': '#f9fafb',
      'editorLineNumber.foreground': '#9ca3af',
      'editor.selectionBackground': '#ff2d2033',
      'editor.lineHighlightBackground': '#00000008'
    }
  });
  monaco.editor.defineTheme('lerd-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: CONFIG_RULES_DARK,
    colors: {
      'editor.background': '#161616',
      'editorGutter.background': '#161616',
      'editorLineNumber.foreground': '#6b7280',
      'editorLineNumber.activeForeground': '#d1d5db',
      'editor.selectionBackground': '#ff2d2055',
      'editor.lineHighlightBackground': '#ffffff0a'
    }
  });
}

let configLangsRegistered = false;
function registerConfigLanguages(monaco: MonacoModule) {
  if (configLangsRegistered) return;
  configLangsRegistered = true;
  for (const { id, def } of configLanguages) {
    monaco.languages.register({ id });
    monaco.languages.setMonarchTokensProvider(id, def as any);
  }
}

export function loadMonaco(): Promise<MonacoModule> {
  if (!monacoPromise) {
    monacoPromise = (async () => {
      const monaco = await import('monaco-editor/esm/vs/editor/editor.api');
      // PHP highlighting for tinker; config-format grammars for the
      // nginx/ini/dotenv editors.
      await import('monaco-editor/esm/vs/basic-languages/php/php.contribution');
      configureWorkers();
      defineThemes(monaco);
      registerConfigLanguages(monaco);
      return monaco;
    })();
  }
  return monacoPromise;
}

// Mirrors the dark/light decision the theme store makes (see
// $stores/theme.ts) without depending on the DOM class having been
// toggled first, so theme switches are race-free.
export function lerdThemeName(t: 'light' | 'dark' | 'auto'): 'lerd-light' | 'lerd-dark' {
  const dark = t === 'dark' || (t === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  return dark ? 'lerd-dark' : 'lerd-light';
}
