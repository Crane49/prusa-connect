<script lang="ts">
  import { t } from '../../i18n';
  import { Separator } from '../../primitives';
  import type { AppFooterProps } from './types';

  // The assistant on the beta portal answers questions while the project is
  // in a closed beta. The repository is not public yet, so it is not linked.
  const SUPPORT = 'https://beta.prusa3d.com/connect-local/assistant';

  let {
    host,
    appVersion,
    apiReference,
    documentation,
    supportUrl,
  }: AppFooterProps = $props();

  const links = $derived([
    ...(apiReference === undefined
      ? []
      : [{ key: 'shell.footer.api' as const, href: apiReference }]),
    ...(documentation === undefined
      ? []
      : [{ key: 'shell.footer.documentation' as const, href: documentation }]),
    { key: 'shell.footer.support' as const, href: supportUrl ?? SUPPORT },
  ]);

  const linkClass =
    'py-1 font-sans text-muted-foreground outline-none transition-colors hover:text-foreground hover:underline focus-visible:bg-accent rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background';
</script>

<footer
  data-slot="app-footer"
  class="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-border px-4 py-3 font-mono text-xs text-muted-foreground @xl/app-shell:px-6"
>
  <div class="flex flex-wrap items-center gap-2">
    {#if host !== undefined}<span>{host}</span>{/if}
    {#if host !== undefined && appVersion !== undefined}
      <Separator orientation="vertical" class="h-3" />
    {/if}
    {#if appVersion !== undefined}
      <span>{t('shell.footer.version', { version: appVersion })}</span>
    {/if}
  </div>
  <nav
    aria-label={t('shell.footer.nav')}
    class="flex flex-wrap items-center gap-x-4"
  >
    {#each links as link (link.key)}
      <a
        href={link.href}
        target="_blank"
        rel="noopener noreferrer"
        class={linkClass}
      >
        {t(link.key)}
      </a>
    {/each}
  </nav>
</footer>
