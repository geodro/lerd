import { describe, it, expect } from 'vitest';
import { compile } from 'monaco-editor/esm/vs/editor/standalone/common/monarch/monarchCompile';
import { MonarchTokenizer } from 'monaco-editor/esm/vs/editor/standalone/common/monarch/monarchLexer';
import { nginxLanguage, iniLanguage, dotenvLanguage } from './monaco-langs';

// Drive the config grammars through Monaco's real Monarch engine so a broken
// tokenizer (invalid action, missing token, swallowed value) fails the build
// instead of silently shipping unhighlighted config editors. Stubs stand in
// for the language/configuration services the lexer constructor wants.
const noop = () => ({ dispose() {} });
const langService = { languageIdCodec: { encodeLanguageId: () => 1 } } as any;
const configService = { getValue: () => ({}), onDidChangeConfiguration: noop, onDidChange: noop } as any;

function tokenTypes(def: any, id: string, line: string): Record<string, string> {
  const lexer = compile(id, structuredClone(def));
  const tokenizer = new MonarchTokenizer(langService, null as any, id, lexer, configService);
  const res = tokenizer.tokenize(line, true, tokenizer.getInitialState());
  // Map start offset -> bare token type (strip the ".lang" postfix).
  const out: Record<string, string> = {};
  for (const t of res.tokens) out[String(t.offset)] = t.type.replace(/\.[a-z]+$/, '');
  return out;
}

describe('config Monarch grammars', () => {
  it('nginx colours block keywords, directives, variables', () => {
    expect(tokenTypes(nginxLanguage, 'nginx', 'server {')['0']).toBe('cfgBlock');
    const listen = tokenTypes(nginxLanguage, 'nginx', '    listen 80;');
    expect(listen['4']).toBe('cfgKey');
    expect(listen['11']).toBe('cfgNumber');
    const proxy = tokenTypes(nginxLanguage, 'nginx', '    proxy_pass http://$host;');
    expect(proxy['4']).toBe('cfgKey');
    expect(Object.values(proxy)).toContain('cfgVar');
  });

  it('ini colours sections, keys, and non-numeric values', () => {
    expect(tokenTypes(iniLanguage, 'ini', '[mysqld]')['0']).toBe('cfgSection');
    const kv = tokenTypes(iniLanguage, 'ini', 'db_host = localhost');
    expect(kv['0']).toBe('cfgKey');
    expect(kv['7']).toBe('cfgOp');
    // The regression we fixed: the value must not fall through to defaultToken.
    expect(kv['10']).toBe('cfgValue');
    expect(tokenTypes(iniLanguage, 'ini', 'max_connections = 200')['18']).toBe('cfgNumber');
  });

  it('dotenv colours keys, operators, and values', () => {
    const env = tokenTypes(dotenvLanguage, 'dotenv', 'APP_ENV=local');
    expect(env['0']).toBe('cfgKey');
    expect(env['7']).toBe('cfgOp');
    expect(env['8']).toBe('cfgValue');
    expect(tokenTypes(dotenvLanguage, 'dotenv', 'APP_KEY="base64:xx"')['8']).toBe('cfgString');
  });
});
