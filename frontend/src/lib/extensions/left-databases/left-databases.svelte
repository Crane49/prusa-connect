<!--
  The databases extensions left behind (ADR-20261021-bqc): what a removal, and an
  update that moved an extension's database, left on the server with the
  extension's data. Each has a purge, which cannot be undone, so the dialog
  asks the person to type the instance's name, and the server purges nothing
  unless the name comes back.

  The section draws nothing while there is nothing left behind.
-->
<script lang="ts">
  import { formatDate, translateFor } from '@prusa/connect-ui/i18n';
  import {
    Button,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    ConfirmDialog,
    FaultAlert,
    Input,
    Label,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { APP_NAMESPACE } from '../../i18n';
  import type {
    LeftDatabase,
    LeftDatabases,
    PurgedDatabase,
  } from '../../stores/left-databases.svelte';
  import { describeExtensionFault } from '../fault';

  let { store }: { store: LeftDatabases } = $props();

  const t = translateFor(APP_NAMESPACE);
  const id = $props.id();

  onMount(() => {
    void store.load();
  });

  // The instance whose purge the dialog asks about, and what was typed.
  let asking = $state<string | undefined>(undefined);
  let typed = $state('');

  const rowsOf = (instance: string): readonly LeftDatabase[] =>
    store.items.filter((one) => one.instance === instance);

  const whereOf = (one: LeftDatabase): string => {
    if (one.engine === 'sqlite') {
      return t('app.settings.extensions.left.where.sqlite', {
        where: one.where,
      });
    }

    return one.provisioned === true
      ? t('app.settings.extensions.left.where.provisioned', {
          where: one.where,
        })
      : t('app.settings.extensions.left.where.postgres', { where: one.where });
  };

  const whyOf = (one: LeftDatabase): string => {
    const values = { instance: one.instance, date: formatDate(one.left_at) };

    switch (one.why) {
      case 'moved':
        return t('app.settings.extensions.left.why.moved', values);
      case 'not_granted':
        return t('app.settings.extensions.left.why.not_granted', values);
      case 'removed':
        return t('app.settings.extensions.left.why.removed', values);
    }
  };

  const purgeNote = (one: LeftDatabase): string => {
    switch (one.purge) {
      case 'drop':
        return t('app.settings.extensions.left.purge.drop');
      case 'delete':
        return t('app.settings.extensions.left.purge.delete');
      case 'forget':
        return t('app.settings.extensions.left.purge.forget');
    }
  };

  const doneOf = (done: PurgedDatabase['done']): string => {
    switch (done) {
      case 'dropped':
        return t('app.settings.extensions.left.done.dropped');
      case 'deleted':
        return t('app.settings.extensions.left.done.deleted');
      case 'forgotten':
        return t('app.settings.extensions.left.done.forgotten');
    }
  };

  const instances = $derived([
    ...new Set(store.items.map((one) => one.instance)),
  ]);

  const purgeFault = $derived(
    store.purgeError === undefined
      ? undefined
      : describeExtensionFault(store.purgeError, t),
  );

  const close = (): void => {
    asking = undefined;
    typed = '';
  };

  const confirm = (): void => {
    const instance = asking;
    const said = typed.trim();
    close();
    if (instance !== undefined) void store.purge(instance, said);
  };
</script>

{#if store.items.length > 0 || purgeFault !== undefined || store.purged.length > 0}
  <Card data-part="left-databases">
    <CardHeader>
      <CardTitle>
        <h2 class="contents">{t('app.settings.extensions.left.title')}</h2>
      </CardTitle>
      <CardDescription>{t('app.settings.extensions.left.note')}</CardDescription
      >
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      {#if purgeFault !== undefined}
        <div class="flex flex-col gap-1" data-part="purge-fault">
          <p class="text-sm font-medium">
            {t('app.settings.extensions.left.purge.failed')}
          </p>
          <FaultAlert fault={purgeFault} />
        </div>
      {/if}

      {#each store.purged as done (done.instance + done.where)}
        <p class="text-sm" role="status" data-part="purged">
          {t('app.settings.extensions.left.purged', {
            done: doneOf(done.done),
            where: done.where,
          })}
        </p>
      {/each}

      <ul class="flex flex-col divide-y">
        {#each instances as instance (instance)}
          <li
            class="flex flex-wrap items-start justify-between gap-3 py-3"
            data-instance={instance}
          >
            <div class="flex min-w-0 flex-col gap-1 text-sm">
              <span class="font-medium">{instance}</span>
              {#each rowsOf(instance) as one (one.where)}
                <span>{whereOf(one)}</span>
                <span class="text-xs text-muted-foreground">{whyOf(one)}</span>
              {/each}
            </div>
            <Button
              variant="destructive"
              size="sm"
              disabled={store.purging !== undefined}
              onclick={() => {
                asking = instance;
              }}
            >
              {t('app.settings.extensions.left.purge')}
            </Button>
          </li>
        {/each}
      </ul>
    </CardContent>
  </Card>
{/if}

<ConfirmDialog
  open={asking !== undefined}
  onOpenChange={(open: boolean) => {
    if (!open) close();
  }}
  title={t('app.settings.extensions.left.purge.title', {
    instance: asking ?? '',
  })}
  description={asking === undefined
    ? ''
    : rowsOf(asking).map(purgeNote).join(' ')}
  confirmLabel={t('app.settings.extensions.left.purge.confirm')}
  onConfirm={confirm}
>
  <div class="flex flex-col gap-1 text-sm" data-part="purge-confirm">
    <Label for={`${id}-confirm`}>
      {t('app.settings.extensions.left.purge.type', {
        instance: asking ?? '',
      })}
    </Label>
    <Input
      id={`${id}-confirm`}
      autocomplete="off"
      spellcheck="false"
      value={typed}
      oninput={(event: Event) => {
        typed = (event.currentTarget as HTMLInputElement).value;
      }}
    />
  </div>
</ConfirmDialog>
