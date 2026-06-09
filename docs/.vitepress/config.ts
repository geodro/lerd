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
      { text: 'Configuration', link: '/configuration' },
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
            { text: 'Windows (WSL2, beta)', link: '/getting-started/wsl2' },
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
            { text: 'Site Groups', link: '/usage/site-groups' },
            { text: 'PHP', link: '/usage/php' },
            { text: 'Node', link: '/usage/node' },
            { text: 'Custom Containers', link: '/usage/custom-containers' },
            { text: 'Host-Proxy Sites', link: '/usage/host-proxy' },
            { text: 'Nginx Overrides', link: '/usage/nginx-overrides' },
          ],
        },
        {
          text: 'Services & Data',
          items: [
            { text: 'Services', link: '/usage/services' },
            { text: 'Service updates', link: '/usage/service-updates' },
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
            { text: 'Framework Commands', link: '/features/commands' },
            { text: 'Framework Definitions', link: '/usage/framework-definitions' },
            { text: 'Queue Workers', link: '/usage/queue-workers' },
            { text: 'Worker Runtime (macOS)', link: '/usage/worker-runtime' },
            { text: 'Healing Failed Workers', link: '/usage/worker-heal' },
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
            { text: 'Tinker tab', link: '/features/tinker' },
            { text: 'Dump viewer', link: '/features/dumps' },
            { text: 'Query viewer', link: '/features/queries' },
            { text: 'Profiler', link: '/features/profiler' },
            { text: 'Notifications', link: '/features/notifications' },
          ],
        },
        {
          text: 'Project lifecycle',
          items: [
            { text: 'Project Setup', link: '/features/project-setup' },
            { text: 'Environment Setup', link: '/features/env-setup' },
            { text: 'FrankenPHP runtime', link: '/features/frankenphp' },
          ],
        },
        {
          text: 'Networking',
          items: [
            { text: 'HTTPS / TLS', link: '/features/https' },
            { text: 'DNS', link: '/features/dns' },
            { text: 'Git Worktrees', link: '/features/git-worktrees' },
          ],
        },
      ],
      '/configuration': [
        {
          text: 'Configuration',
          items: [
            { text: 'Overview', link: '/configuration' },
            { text: 'Per-project (.lerd.yaml)', link: '/configuration#per-project-config-lerdyaml' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/configuration' },
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
            { text: 'Configuration', link: '/configuration' },
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
      { icon: 'discord', link: 'https://discord.gg/ej33c5N9s' },
      {
        icon: {
          svg: '<svg role="img" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><title>Reddit</title><path d="M12 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0zm5.01 4.744c.688 0 1.25.561 1.25 1.249a1.25 1.25 0 0 1-2.498.056l-2.597-.547-.8 3.747c1.824.07 3.48.632 4.674 1.488.308-.309.73-.491 1.207-.491.968 0 1.754.786 1.754 1.754 0 .716-.435 1.333-1.01 1.614a3.111 3.111 0 0 1 .042.52c0 2.694-3.13 4.87-7.004 4.87-3.874 0-7.004-2.176-7.004-4.87 0-.183.015-.366.043-.534A1.748 1.748 0 0 1 4.028 12c0-.968.786-1.754 1.754-1.754.463 0 .898.196 1.207.49 1.207-.883 2.878-1.43 4.744-1.487l.885-4.182a.342.342 0 0 1 .14-.197.35.35 0 0 1 .238-.042l2.906.617a1.214 1.214 0 0 1 1.108-.701zM9.25 12C8.561 12 8 12.562 8 13.25c0 .687.561 1.248 1.25 1.248.687 0 1.248-.561 1.248-1.249 0-.688-.561-1.249-1.249-1.249zm5.5 0c-.687 0-1.248.561-1.248 1.25 0 .687.561 1.248 1.249 1.248.688 0 1.249-.561 1.249-1.249 0-.687-.562-1.249-1.25-1.249zm-5.466 3.99a.327.327 0 0 0-.231.094.33.33 0 0 0 0 .463c.842.842 2.484.913 2.961.913.477 0 2.105-.056 2.961-.913a.361.361 0 0 0 .029-.463.33.33 0 0 0-.464 0c-.547.533-1.684.73-2.512.73-.828 0-1.979-.196-2.512-.73a.326.326 0 0 0-.232-.095z"/></svg>',
        },
        link: 'https://reddit.com/r/lerd',
      },
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
