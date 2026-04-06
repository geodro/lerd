import { defineConfig } from 'vitepress'

const SITE_URL = 'https://geodro.github.io/lerd'
const OG_IMAGE = `${SITE_URL}/assets/social-preview.png`

export default defineConfig({
  title: 'Lerd',
  description: 'Open-source Herd-like local PHP development environment for Linux. Automatic .test domains, PHP 8.2–8.4, rootless Podman. Works on Ubuntu, Fedora, Arch, and Debian.',
  base: '/lerd/',
  cleanUrls: true,

  sitemap: {
    hostname: SITE_URL,
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
      ],
      '/usage/': [
        {
          text: 'Usage',
          items: [
            { text: 'Site Management', link: '/usage/sites' },
            { text: 'PHP', link: '/usage/php' },
            { text: 'Node', link: '/usage/node' },
            { text: 'Services', link: '/usage/services' },
            { text: 'Database', link: '/usage/database' },
            { text: 'Frameworks & Workers', link: '/usage/frameworks' },
            { text: 'Queue Workers', link: '/usage/queue-workers' },
            { text: 'Stripe', link: '/usage/stripe' },
          ],
        },
      ],
      '/features/': [
        {
          text: 'Features',
          items: [
            { text: 'Web UI', link: '/features/web-ui' },
            { text: 'System Tray', link: '/features/system-tray' },
            { text: 'HTTPS / TLS', link: '/features/https' },
            { text: 'Git Worktrees', link: '/features/git-worktrees' },
            { text: 'Project Setup', link: '/features/project-setup' },
            { text: 'Environment Setup', link: '/features/env-setup' },
            { text: 'AI Integration (MCP)', link: '/features/mcp' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/reference/configuration' },
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
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
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
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
