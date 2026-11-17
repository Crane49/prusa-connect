<script lang="ts">
  import {
    describeFault,
    formatBytes,
    formatDateTime,
    formatNumber,
    t,
  } from '../../i18n';
  import {
    Button,
    Card,
    CardContent,
    Checkbox,
    ConfirmDialog,
    FaultAlert,
    Label,
    SettingsPane,
    Skeleton,
  } from '../../primitives';
  import type { BackupManifest } from '../../data';
  import type { SettingsBackupsScreenProps } from './types';

  let { source }: SettingsBackupsScreenProps = $props();

  let postgresAck = $state(false);
  let restoring = $state<BackupManifest | undefined>(undefined);
  let deleting = $state<BackupManifest | undefined>(undefined);

  const working = $derived(source.busy !== undefined);
  const backupFault = $derived(
    source.backupError === undefined
      ? undefined
      : describeFault(source.backupError),
  );

  const backupSize = (backup: BackupManifest): number =>
    (backup.files ?? []).reduce((total, file) => total + file.size, 0) +
    (backup.postgres ?? []).reduce(
      (total, entry) => total + (entry.size ?? 0),
      0,
    );
</script>

<SettingsPane
  class="w-full"
  title={t('settingsMaintenance.backups.heading')}
  description={t('settingsMaintenance.backups.description')}
>
  <div class="flex flex-col gap-5 pb-6">
    <Card>
      <CardContent class="flex flex-col gap-3">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 class="text-sm font-semibold">
              {t('settingsMaintenance.backups.createHeading')}
            </h3>
            <p class="text-sm text-muted-foreground">
              {t('settingsMaintenance.backups.createDescription')}
            </p>
          </div>
          <Button
            disabled={working}
            onclick={() =>
              void source.createBackup(source.upgrade?.to_version, postgresAck)}
          >
            {t('settingsMaintenance.backups.create')}
          </Button>
        </div>
        <!-- Asked only where a PostgreSQL database lies outside the backup. -->
        {#if source.version?.postgres_backup_ack_required === true}
          <Label variant="option">
            <Checkbox bind:checked={postgresAck} class="mt-0.5" />
            <span>{t('settingsMaintenance.apply.postgres')}</span>
          </Label>
        {/if}
      </CardContent>
    </Card>

    {#if source.backupLoading}
      <div class="flex flex-col gap-2" aria-busy="true">
        <Skeleton class="h-18 w-full" />
        <Skeleton class="h-18 w-full" />
      </div>
    {:else if backupFault}
      <div class="flex flex-col gap-3">
        <FaultAlert fault={backupFault} />
        <Button
          class="self-start"
          variant="secondary"
          disabled={working}
          onclick={() => void source.refreshBackups()}
        >
          {t('settingsMaintenance.backups.retry')}
        </Button>
      </div>
    {:else if source.backups.length === 0}
      <div class="border-y border-border py-6">
        <p class="text-sm font-medium">
          {t('settingsMaintenance.backups.empty')}
        </p>
        <p class="mt-1 text-sm text-muted-foreground">
          {t('settingsMaintenance.backups.emptyAction')}
        </p>
      </div>
    {:else}
      <ul class="divide-y divide-border border-y border-border">
        {#each source.backups as backup (backup.id)}
          <li
            class="flex flex-col gap-3 py-4 @md/pane:flex-row @md/pane:items-center @md/pane:justify-between"
          >
            <div class="min-w-0 text-sm">
              <p class="truncate font-mono font-medium">{backup.id}</p>
              <p class="mt-1 text-xs text-muted-foreground">
                {formatDateTime(backup.created_at)}
                · {formatBytes(backupSize(backup))}
                · {formatNumber((backup.files ?? []).length)}
                {t('settingsMaintenance.backups.files')}
              </p>
              {#if (backup.from_version ?? backup.to_version) !== undefined}
                <p class="mt-1 text-xs text-muted-foreground">
                  {backup.from_version ?? '—'} → {backup.to_version ?? '—'}
                </p>
              {/if}
            </div>
            <div class="flex shrink-0 gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={working}
                onclick={() => {
                  restoring = backup;
                }}
              >
                {t('settingsMaintenance.backups.restore')}
              </Button>
              <Button
                size="sm"
                variant="destructive"
                disabled={working}
                onclick={() => {
                  deleting = backup;
                }}
              >
                {t('settingsMaintenance.backups.delete')}
              </Button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</SettingsPane>

{#if restoring}
  <ConfirmDialog
    open
    onOpenChange={(open) => {
      if (!open) restoring = undefined;
    }}
    title={t('settingsMaintenance.backups.restoreTitle')}
    description={t('settingsMaintenance.backups.restoreDescription', {
      id: restoring.id,
    })}
    confirmLabel={t('settingsMaintenance.backups.restore')}
    onConfirm={() => {
      if (restoring) void source.restoreBackup(restoring.id);
      restoring = undefined;
    }}
  />
{/if}

{#if deleting}
  <ConfirmDialog
    open
    onOpenChange={(open) => {
      if (!open) deleting = undefined;
    }}
    title={t('settingsMaintenance.backups.deleteTitle')}
    description={t('settingsMaintenance.backups.deleteDescription', {
      id: deleting.id,
    })}
    confirmLabel={t('settingsMaintenance.backups.delete')}
    onConfirm={() => {
      if (deleting) void source.deleteBackup(deleting.id);
      deleting = undefined;
    }}
  />
{/if}
