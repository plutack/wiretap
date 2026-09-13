import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: process.env.SITE_URL,
  integrations: [
    starlight({
      title: 'Wiretap',
      description: 'Capture HTTP traffic and receive public webhooks without exposing your development machine.',
      logo: {
        src: './src/assets/wiretap.svg',
        replacesTitle: false,
      },
      favicon: '/favicon.svg',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/plutack/wiretap',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/plutack/wiretap/edit/main/docs/src/content/docs/',
      },
      customCss: ['./src/styles/custom.css'],
      sidebar: [
        {
          label: 'Get started',
          items: [
            { label: 'Install Wiretap', slug: 'getting-started/install' },
            { label: 'Capture your first request', slug: 'getting-started/quick-start' },
            { label: 'Receive your first webhook', slug: 'getting-started/first-webhook' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'How Wiretap works', slug: 'concepts/how-wiretap-works' },
            { label: 'Desktop identities and projects', slug: 'concepts/identities-and-projects' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'Intercept local traffic', slug: 'guides/intercept-traffic' },
            { label: 'Compose and replay requests', slug: 'guides/request-composer' },
            { label: 'Transform payloads', slug: 'guides/transforms' },
            { label: 'Host the relay', slug: 'guides/host-relay' },
            { label: 'Manage the relay', slug: 'guides/manage-relay' },
            { label: 'Customize the desktop', slug: 'guides/desktop-themes' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Configuration', slug: 'reference/configuration' },
            { label: 'CLI', slug: 'reference/cli' },
            { label: 'Transform API', slug: 'reference/transform-api' },
            { label: 'Limits and security', slug: 'reference/limits-and-security' },
          ],
        },
        { label: 'Troubleshooting', slug: 'troubleshooting' },
        { label: 'Releases and downloads', slug: 'releases' },
      ],
    }),
  ],
});
