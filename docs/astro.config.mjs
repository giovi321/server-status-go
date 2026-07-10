import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://giovi321.github.io',
  base: '/server-status-go',
  integrations: [
    starlight({
      title: 'server-status',
      description: 'Single-binary host metrics agent for MQTT and Home Assistant — documentation',
      components: {
        Head: './src/components/Head.astro',
      },
      customCss: ['./src/styles/diagrams.css'],
      head: [
        {
          tag: 'link',
          attrs: { rel: 'preconnect', href: 'https://fonts.googleapis.com' },
        },
        {
          tag: 'link',
          attrs: { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'stylesheet',
            href: 'https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500;600&display=swap',
          },
        },
      ],
      logo: {
        src: './src/assets/logo.svg',
        replacesTitle: false,
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/giovi321/server-status-go',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/giovi321/server-status-go/edit/main/docs/',
      },
      sidebar: [
        { label: 'Home', link: '/' },
        {
          label: 'Getting started',
          items: [
            { label: 'Installation', link: '/getting-started/installation/' },
            { label: 'Configuration', link: '/getting-started/configuration/' },
            { label: 'Running the agent', link: '/getting-started/running/' },
          ],
        },
        {
          label: 'Home Assistant',
          items: [
            { label: 'Discovery and devices', link: '/home-assistant/discovery/' },
            { label: 'Device hierarchy', link: '/home-assistant/hierarchy/' },
            { label: 'Entity naming', link: '/home-assistant/naming/' },
          ],
        },
        {
          label: 'Metrics',
          items: [
            { label: 'Collectors', link: '/metrics/collectors/' },
            { label: 'Metrics reference', link: '/metrics/reference/' },
            { label: 'Rsnapshot backups', link: '/metrics/rsnapshot/' },
          ],
        },
        {
          label: 'Control and updates',
          items: [
            { label: 'HTTP control API', link: '/control/http/' },
            { label: 'MQTT commands', link: '/control/mqtt/' },
            { label: 'Self-update', link: '/control/self-update/' },
            { label: 'Webhook sink', link: '/control/webhook/' },
          ],
        },
        {
          label: 'Reliability',
          items: [
            { label: 'systemd and watchdog', link: '/reliability/systemd/' },
            { label: 'Clean uninstall', link: '/reliability/purge/' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'CLI flags', link: '/reference/cli/' },
            { label: 'Configuration reference', link: '/reference/config/' },
            { label: 'MQTT topics', link: '/reference/topics/' },
          ],
        },
        {
          label: 'Development',
          items: [
            { label: 'Building and testing', link: '/development/building/' },
            { label: 'Releasing', link: '/development/releasing/' },
            { label: 'Migrating from the Python tool', link: '/development/migration/' },
          ],
        },
      ],
    }),
  ],
});
