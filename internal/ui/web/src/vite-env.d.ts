/// <reference types="vite/client" />

// Monaco ships its types only on the package root, not on the deep ESM
// entrypoints we import to keep the bundle lean. Re-point them here.
declare module 'monaco-editor/esm/vs/editor/editor.api' {
  export * from 'monaco-editor';
}
declare module 'monaco-editor/esm/vs/basic-languages/php/php.contribution';

// Internal Monarch entrypoints, used only by the grammar tests to tokenise
// the config languages through Monaco's real engine.
declare module 'monaco-editor/esm/vs/editor/standalone/common/monarch/monarchCompile' {
  export function compile(languageId: string, json: unknown): unknown;
}
declare module 'monaco-editor/esm/vs/editor/standalone/common/monarch/monarchLexer' {
  export class MonarchTokenizer {
    constructor(languageService: unknown, themeService: unknown, languageId: string, lexer: unknown, configurationService: unknown);
    getInitialState(): unknown;
    tokenize(line: string, hasEOL: boolean, state: unknown): { tokens: { offset: number; type: string }[]; endState: unknown };
  }
}
