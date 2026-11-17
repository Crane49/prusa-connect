<script lang="ts">
  import type { Snippet } from 'svelte';

  import { t } from '../../i18n';
  import { NavTabs } from '../../primitives';
  import type { ServerSettingsTab } from './types';

  let {
    active,
    overviewHref,
    healthHref,
    updatesHref,
    backupsHref,
    networkHref,
    children,
  }: {
    active: ServerSettingsTab;
    overviewHref: string;
    healthHref: string;
    /** Absent where core.upgrade.enabled is off, and the tab is not drawn. */
    updatesHref?: string | undefined;
    /** Absent where core.upgrade.enabled is off, and the tab is not drawn. */
    backupsHref?: string | undefined;
    networkHref: string;
    children: Snippet;
  } = $props();

  const tabs = $derived([
    {
      id: 'overview' as const,
      href: overviewHref,
      label: t('settingsServer.tab.overview'),
    },
    {
      id: 'health' as const,
      href: healthHref,
      label: t('settingsServer.tab.health'),
    },
    ...(updatesHref === undefined
      ? []
      : [
          {
            id: 'updates' as const,
            href: updatesHref,
            label: t('settingsServer.tab.updates'),
          },
        ]),
    ...(backupsHref === undefined
      ? []
      : [
          {
            id: 'backups' as const,
            href: backupsHref,
            label: t('settingsServer.tab.backups'),
          },
        ]),
    {
      id: 'network' as const,
      href: networkHref,
      label: t('settingsServer.tab.network'),
    },
  ]);
</script>

<div class="flex w-full flex-col gap-4">
  <section
    class="rounded-panel @container/server-shell overflow-hidden bg-card text-card-foreground"
    aria-labelledby="server-settings-title"
  >
    <header class="flex flex-col gap-3 p-4 @md/server-shell:p-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <h2 id="server-settings-title" class="text-lead font-semibold">
            {t('settingsServer.title')}
          </h2>
          <p class="text-body text-muted-foreground">
            {updatesHref === undefined
              ? t('settingsServer.descriptionWithoutUpdates')
              : t('settingsServer.description')}
          </p>
        </div>
      </div>
    </header>

    <div class="px-4 @md/server-shell:px-6">
      <NavTabs label={t('settingsServer.tabs.label')} {tabs} {active} />
    </div>
  </section>

  {@render children()}
</div>
