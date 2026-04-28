// Heroicons-style outline paths keyed by service family. Returns an HTML
// fragment containing `<path>` and possibly `<circle>` elements, ready to be
// used as children of an `<svg>`.

const ICONS: Record<string, string> = {
  database:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 7v10c0 1.657 3.582 3 8 3s8-1.343 8-3V7M4 7c0 1.657 3.582 3 8 3s8-1.343 8-3M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3m0 5c0 1.657-3.582 3-8 3s-8-1.343-8-3"/>',
  mail:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 8l9 6 9-6M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/>',
  search:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>',
  storage:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/>',
  windowIcon:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 6a2 2 0 012-2h12a2 2 0 012 2v12a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM4 9h16"/>',
  leaf:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 21c0-6 0-12 0-18c-4 3-7 7-7 11a7 7 0 007 7zm0 0a7 7 0 007-7c0-4-3-8-7-11"/>',
  browserPlay:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 6a2 2 0 012-2h12a2 2 0 012 2v12a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM4 9h16M10 13l4 2.5L10 18v-5z"/>',
  elephant:
    '<circle cx="12" cy="10" r="7" stroke-width="1.5"/><circle cx="3.5" cy="9" r="3.5" stroke-width="1.5"/><circle cx="20.5" cy="9" r="3.5" stroke-width="1.5"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M10 17 Q9 21 12 22 Q15 21 14 17"/><circle cx="9.5" cy="10" r="1" fill="currentColor" stroke="none"/><circle cx="14.5" cy="10" r="1" fill="currentColor" stroke="none"/>',
  docs:
    '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253"/>'
};

const BY_NAME: Record<string, string> = {
  phpmyadmin: ICONS.database,
  pgadmin: ICONS.elephant,
  adminer: ICONS.database,
  mailpit: ICONS.mail,
  mailhog: ICONS.mail,
  meilisearch: ICONS.search,
  elasticsearch: ICONS.search,
  elasticvue: ICONS.search,
  typesense: ICONS.search,
  rustfs: ICONS.storage,
  minio: ICONS.storage,
  'mongo-express': ICONS.leaf,
  mongo: ICONS.leaf,
  selenium: ICONS.browserPlay,
  docs: ICONS.docs
};

export function dashboardIconSvg(name: string): string {
  return BY_NAME[name] || ICONS.windowIcon;
}
