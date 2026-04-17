import { defineConfig } from 'vitepress'

const SITE_URL = 'https://geodro.github.io/lerd'
const OG_IMAGE = `${SITE_URL}/assets/social-preview.png`

export default defineConfig({
  title: 'Lerd',
  description: 'Open-source Herd-like local PHP development environment for Linux. Automatic .test domains, PHP 8.2–8.5, rootless Podman. Works on Ubuntu, Fedora, Arch, and Debian.',
  base: '/lerd/',
  cleanUrls: true,

  sitemap: {
    hostname: SITE_URL,
    transformItems(items) {
      return items.map(item => ({ ...item, url: `lerd/${item.url}` }))
    },
  },

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/lerd/assets/logo.svg' }],

    // Open Graph
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Lerd' }],
    ['meta', { property: 'og:image', content: OG_IMAGE }],
    ['meta', { property: 'og:image:width', content: '1200' }],
    ['meta', { property: 'og:image:height', content: '630' }],

    // Twitter / X
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:image', content: OG_IMAGE }],
  ],

  transformPageData(pageData, { siteConfig }) {
    const canonicalUrl = `${SITE_URL}/${pageData.relativePath.replace(/\.md$/, '').replace(/index$/, '')}`
    const description = pageData.frontmatter.description ?? pageData.description ?? siteConfig.site.description
    const title = pageData.frontmatter.title ?? pageData.title ?? siteConfig.site.title
    pageData.frontmatter.head ??= []
    pageData.frontmatter.head.push(
      ['link', { rel: 'canonical', href: canonicalUrl }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: canonicalUrl }],
      ['meta', { name: 'description', content: description }],
    )
  },

  themeConfig: {
    logo: '/assets/logo.svg',
    siteTitle: 'Lerd',

    nav: [
      { text: 'Getting Started', link: '/getting-started/requirements' },
      { text: 'Usage', link: '/usage/sites' },
      { text: 'Features', link: '/features/web-ui' },
      { text: 'Reference', link: '/reference/commands' },
      { text: 'Contributing', link: '/contributing/building' },
      { text: 'Changelog', link: '/changelog' },
    ],

    sidebar: {
      '/getting-started/': [
        {
          text: 'Getting Started',
          items: [
            { text: 'Requirements', link: '/getting-started/requirements' },
            { text: 'Installation', link: '/getting-started/installation' },
            { text: 'Quick Start', link: '/getting-started/quick-start' },
            { text: 'Comparison', link: '/getting-started/comparison' },
          ],
        },
        {
          text: 'Framework walkthroughs',
          items: [
            { text: 'Laravel', link: '/getting-started/laravel' },
            { text: 'Symfony', link: '/getting-started/symfony' },
            { text: 'WordPress', link: '/getting-started/wordpress' },
            { text: 'Containers (Node, Python, Go, …)', link: '/getting-started/containers' },
          ],
        },
        {
          text: 'Add-ons',
          items: [
            { text: 'Services (MongoDB, phpMyAdmin, …)', link: '/getting-started/services' },
          ],
        },
      ],
      '/usage/': [
        {
          text: 'Lifecycle',
          items: [
            { text: 'Start, Stop & Autostart', link: '/usage/lifecycle' },
          ],
        },
        {
          text: 'Sites & Runtimes',
          items: [
            { text: 'Site Management', link: '/usage/sites' },
            { text: 'PHP', link: '/usage/php' },
            { text: 'Node', link: '/usage/node' },
            { text: 'Custom Containers', link: '/usage/custom-containers' },
          ],
        },
        {
          text: 'Services & Data',
          items: [
            { text: 'Services', link: '/usage/services' },
            { text: 'Service presets', link: '/usage/service-presets' },
            { text: 'Custom services', link: '/usage/custom-services' },
            { text: 'Database', link: '/usage/database' },
          ],
        },
        {
          text: 'Frameworks & Workers',
          items: [
            { text: 'Frameworks', link: '/usage/frameworks' },
            { text: 'Framework Workers', link: '/usage/framework-workers' },
            { text: 'Framework Definitions', link: '/usage/framework-definitions' },
            { text: 'Queue Workers', link: '/usage/queue-workers' },
            { text: 'Worker Runtime (macOS)', link: '/usage/worker-runtime' },
            { text: 'Browser Testing', link: '/usage/browser-testing' },
          ],
        },
        {
          text: 'Integrations & Migration',
          items: [
            { text: 'Stripe', link: '/usage/stripe' },
            { text: 'LAN sharing', link: '/usage/lan-sharing' },
            { text: 'Remote / LAN Development', link: '/usage/remote-development' },
            { text: 'Importing from Sail', link: '/usage/import-sail' },
          ],
        },
      ],
      '/features/': [
        {
          text: 'UI & AI',
          items: [
            { text: 'Web UI', link: '/features/web-ui' },
            { text: 'Terminal Dashboard', link: '/features/tui' },
            { text: 'System Tray', link: '/features/system-tray' },
            { text: 'AI Integration (MCP)', link: '/features/mcp' },
          ],
        },
        {
          text: 'Project lifecycle',
          items: [
            { text: 'Project Setup', link: '/features/project-setup' },
            { text: 'Environment Setup', link: '/features/env-setup' },
          ],
        },
        {
          text: 'Networking',
          items: [
            { text: 'HTTPS / TLS', link: '/features/https' },
            { text: 'Git Worktrees', link: '/features/git-worktrees' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/reference/configuration' },
          ],
        },
        {
          text: 'Internals',
          items: [
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
          ],
        },
        {
          text: 'Help',
          items: [
            { text: 'Troubleshooting', link: '/troubleshooting' },
          ],
        },
      ],
      '/troubleshooting': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/reference/configuration' },
          ],
        },
        {
          text: 'Internals',
          items: [
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
          ],
        },
        {
          text: 'Help',
          items: [
            { text: 'Troubleshooting', link: '/troubleshooting' },
          ],
        },
      ],
      '/contributing/': [
        {
          text: 'Contributing',
          items: [
            { text: 'Building from Source', link: '/contributing/building' },
            { text: 'Pull Requests', link: '/contributing/pull-requests' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/geodro/lerd' },
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Lerd',
    },

    search: {
      provider: 'local',
    },

    editLink: {
      pattern: 'https://github.com/geodro/lerd/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
  },
})
