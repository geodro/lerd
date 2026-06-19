// Lightweight, line-oriented Monarch grammars for the config editors. None
// push tokenizer states, so each line is tokenised from `root`, which is safe
// for these line-based formats. Kept in their own module (no Monaco worker
// import) so the grammar tests can tokenise the exact definitions the editors
// register. Token names are `cfg*` so they don't recolour the PHP editor.

export const nginxLanguage = {
  defaultToken: '',
  blocks: [
    'server', 'location', 'http', 'events', 'upstream', 'if', 'map', 'types',
    'stream', 'mail', 'geo', 'split_clients', 'limit_except'
  ],
  tokenizer: {
    root: [
      [/#.*$/, 'cfgComment'],
      [/^(\s*)([A-Za-z_]\w*)/, ['', { cases: { '@blocks': 'cfgBlock', '@default': 'cfgKey' } }]],
      [/\$[A-Za-z_]\w*/, 'cfgVar'],
      [/"([^"\\]|\\.)*"/, 'cfgString'],
      [/'([^'\\]|\\.)*'/, 'cfgString'],
      [/\b\d[\w.]*\b/, 'cfgNumber'],
      [/[{};]/, 'cfgOp']
    ]
  }
};

// ini-style tuning files (php.ini, my.cnf) plus redis-style `directive arg`
// configs. Sections and `key = value` match first, the bare-directive form is
// the fallback, and the trailing cfgValue rule colours non-numeric values
// (paths, hostnames, on/off) the number and string rules don't catch.
export const iniLanguage = {
  defaultToken: '',
  tokenizer: {
    root: [
      [/^\s*[#;].*$/, 'cfgComment'],
      [/^\s*\[[^\]]*\]\s*$/, 'cfgSection'],
      [/^(\s*)([A-Za-z_][\w.-]*)(\s*=\s*)/, ['', 'cfgKey', 'cfgOp']],
      [/^(\s*)([A-Za-z_][\w.-]*)(\s+)/, ['', 'cfgKey', '']],
      [/[#;].*$/, 'cfgComment'],
      [/"[^"]*"/, 'cfgString'],
      [/'[^']*'/, 'cfgString'],
      [/\b\d[\w.]*\b/, 'cfgNumber'],
      [/[^#;]+/, 'cfgValue']
    ]
  }
};

export const dotenvLanguage = {
  defaultToken: '',
  tokenizer: {
    root: [
      [/^\s*#.*$/, 'cfgComment'],
      [/^(\s*)([A-Za-z_]\w*)(\s*=\s*)/, ['', 'cfgKey', 'cfgOp']],
      [/"[^"]*"/, 'cfgString'],
      [/'[^']*'/, 'cfgString'],
      [/.+$/, 'cfgValue']
    ]
  }
};

export const configLanguages = [
  { id: 'nginx', def: nginxLanguage },
  { id: 'ini', def: iniLanguage },
  { id: 'dotenv', def: dotenvLanguage }
] as const;
