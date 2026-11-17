<script lang="ts">
  import { describeFault, formatDateTime, t } from '../../i18n';
  import {
    FaultAlert,
    SettingsPane,
    Skeleton,
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
  } from '../../primitives';
  import OfflineUpdate from './offline-update.svelte';
  import OnlineUpdate from './online-update.svelte';
  import type { SettingsUpdatesScreenProps } from './types';
  import ExternalRestore from './external-restore.svelte';
  import UpdateTransaction from './update-transaction.svelte';

  let { source, method = $bindable('online') }: SettingsUpdatesScreenProps =
    $props();

  const fault = $derived(
    source.error === undefined ? undefined : describeFault(source.error),
  );
</script>

<SettingsPane
  class="w-full"
  title={t('settingsMaintenance.updates.title')}
  description={t('settingsMaintenance.updates.description')}
>
  {#if !source.loaded}
    <div
      class="flex flex-col gap-3 pb-6"
      aria-busy="true"
      aria-label={t('settingsMaintenance.loading')}
    >
      <Skeleton class="h-24 w-full" />
      <Skeleton class="h-40 w-full" />
    </div>
  {:else}
    <div class="flex flex-col gap-6 pb-6">
      <FaultAlert {fault} />

      <UpdateTransaction {source} />

      {#if source.version}
        <section
          class="grid gap-3 border-b border-border pb-6 @md/pane:grid-cols-4"
          aria-labelledby="version-heading"
        >
          <h3
            id="version-heading"
            class="text-sm font-semibold @md/pane:col-span-4"
          >
            {t('settingsMaintenance.version.heading')}
          </h3>
          <dl class="contents text-sm">
            <div>
              <dt class="text-xs text-muted-foreground">
                {t('settingsMaintenance.version.current')}
              </dt>
              <dd class="font-mono font-medium">{source.version.version}</dd>
            </div>
            <div>
              <dt class="text-xs text-muted-foreground">
                {t('settingsMaintenance.version.platform')}
              </dt>
              <dd class="font-mono">
                {source.version.goos}/{source.version.goarch}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-muted-foreground">
                {t('settingsMaintenance.version.profile')}
              </dt>
              <dd>
                {source.version.build_profile}/{source.version.build_variant}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-muted-foreground">
                {t('settingsMaintenance.version.management')}
              </dt>
              <dd>
                {source.version.managed_externally
                  ? t('settingsMaintenance.version.managed')
                  : t('settingsMaintenance.version.local')}
              </dd>
            </div>
          </dl>
          {#if source.version.managed_reason}
            <p class="rounded-card bg-muted p-3 text-sm @md/pane:col-span-4">
              {source.version.managed_reason}
            </p>
          {/if}
        </section>
      {/if}

      <Tabs bind:value={method}>
        <TabsList aria-label={t('settingsMaintenance.method.label')}>
          <TabsTrigger value="online">
            {t('settingsMaintenance.method.online')}
          </TabsTrigger>
          <TabsTrigger value="offline">
            {t('settingsMaintenance.method.offline')}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="online" class="pt-4">
          <OnlineUpdate {source} />
        </TabsContent>
        <TabsContent value="offline" class="pt-4">
          <OfflineUpdate {source} />
        </TabsContent>
      </Tabs>

      <section class="flex flex-col gap-3" aria-labelledby="history-heading">
        <h3 id="history-heading" class="text-sm font-semibold">
          {t('settingsMaintenance.history.heading')}
        </h3>
        {#if source.history.length === 0}
          <p class="text-sm text-muted-foreground">
            {t('settingsMaintenance.history.empty')}
          </p>
        {:else}
          <ul class="divide-y divide-border border-y border-border">
            {#each source.history as entry (entry.id)}
              <li
                class="grid gap-1 py-3 text-sm @md/pane:grid-cols-3 @md/pane:items-center"
              >
                <span>
                  <span class="font-mono">{entry.from_version}</span>
                  →
                  <span class="font-mono">{entry.to_version}</span>
                </span>
                <span>{entry.state}</span>
                <time
                  class="text-muted-foreground @md/pane:text-end"
                  datetime={entry.updated_at}
                >
                  {formatDateTime(entry.updated_at)}
                </time>
                <ExternalRestore
                  class="@md/pane:col-span-3"
                  databases={entry.external_restore ?? []}
                />
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  {/if}
</SettingsPane>
