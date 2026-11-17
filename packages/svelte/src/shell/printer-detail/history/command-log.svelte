<!--
  Every command this server stored for the printer, newest first: what was
  asked, who asked, how it ended and how long it took. Opening a row reads
  that command again and draws its lifecycle.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
  import DatabaseXIcon from '@lucide/svelte/icons/database-x';
  import InboxIcon from '@lucide/svelte/icons/inbox';
  import { onMount } from 'svelte';

  import type { CommandRecord } from '../../../data';
  import { describeFault, formatDateTime, t } from '../../../i18n';
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    EmptyState,
    FaultAlert,
    ListingSkeleton,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '../../../primitives';
  import { verbText } from '../commands/vocabulary';
  import CommandLifecycle from './command-lifecycle.svelte';
  import {
    elapsedText,
    RESULT_VARIANT,
    resultOf,
    resultText,
  } from './command-status';
  import type { CommandLogProps } from './types';

  let { source }: CommandLogProps = $props();

  // What the core answers when a deployment declares no history store.
  const HISTORY_NOT_CONFIGURED_FAULT = 'core.resource/HISTORY_NOT_CONFIGURED';

  // Which rows are open. Client state, so it stays in the component.
  let opened = $state<readonly string[]>([]);

  onMount(() => {
    void source.load();

    return () => {
      source.abort();
    };
  });

  const refused = $derived(source.error?.code === HISTORY_NOT_CONFIGURED_FAULT);
  const fault = $derived(
    source.error === undefined || refused
      ? undefined
      : describeFault(source.error),
  );

  const whenOf = (record: CommandRecord): string => {
    const at = record.requested_at ?? record.settled_at;

    return at === undefined ? '' : formatDateTime(at);
  };

  const tookText = (record: CommandRecord): string =>
    record.status === undefined && record.outcome_unknown !== true
      ? ''
      : elapsedText(record.elapsed_ms);

  const toggle = (id: string): void => {
    if (opened.includes(id)) {
      opened = opened.filter((one) => one !== id);

      return;
    }
    opened = [...opened, id];
    void source.open(id);
  };
</script>

<Card>
  <CardHeader>
    <CardTitle>
      <h2 class="contents">{t('printerDetailHistory.commands.heading')}</h2>
    </CardTitle>
  </CardHeader>
  <CardContent class="flex flex-col gap-4">
    {#if !source.loaded}
      <ListingSkeleton
        label={t('printerDetailHistory.commands.loading')}
        rows={5}
      />
    {:else if refused}
      <EmptyState
        icon={DatabaseXIcon}
        heading={t('printerDetailHistory.unavailable.heading')}
        headingLevel="h3"
        description={t('printerDetailHistory.unavailable.note')}
      />
    {:else}
      <!-- A failed `Read more` keeps the rows it already had. -->
      {#if fault}
        <FaultAlert {fault} />
      {/if}
      {#if source.rows.length === 0 && fault === undefined}
        <EmptyState
          icon={InboxIcon}
          heading={t('printerDetailHistory.commands.empty.heading')}
          headingLevel="h3"
        />
      {:else if source.rows.length > 0}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead class="w-0"
                ><span class="sr-only"
                  >{t('printerDetailHistory.commands.column.details')}</span
                ></TableHead
              >
              <TableHead
                >{t('printerDetailHistory.commands.column.time')}</TableHead
              >
              <TableHead
                >{t('printerDetailHistory.commands.column.command')}</TableHead
              >
              <TableHead
                >{t('printerDetailHistory.commands.column.by')}</TableHead
              >
              <TableHead
                >{t('printerDetailHistory.commands.column.result')}</TableHead
              >
              <TableHead class="text-end"
                >{t('printerDetailHistory.commands.column.elapsed')}</TableHead
              >
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each source.rows as row (row.command_id)}
              {@const open = opened.includes(row.command_id)}
              {@const result = resultOf(row)}
              <TableRow>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-expanded={open}
                    label={open
                      ? t('printerDetailHistory.commands.collapse', {
                          command: verbText(row.verb),
                        })
                      : t('printerDetailHistory.commands.expand', {
                          command: verbText(row.verb),
                        })}
                    onclick={() => toggle(row.command_id)}
                  >
                    <ChevronRightIcon
                      aria-hidden="true"
                      class={['transition-transform', open && 'rotate-90']}
                    />
                  </Button>
                </TableCell>
                <TableCell class="text-xs tabular-nums">{whenOf(row)}</TableCell
                >
                <TableCell class="text-sm">{verbText(row.verb)}</TableCell>
                <TableCell
                  class={row.auth_id === undefined
                    ? 'text-xs text-muted-foreground'
                    : 'font-mono text-xs'}
                >
                  {row.auth_id ??
                    t('printerDetailHistory.commands.detail.absent')}
                </TableCell>
                <TableCell>
                  <Badge variant={RESULT_VARIANT[result]}>
                    {resultText(row)}
                  </Badge>
                </TableCell>
                <TableCell class="text-end text-xs tabular-nums">
                  {tookText(row)}
                </TableCell>
              </TableRow>
              {#if open}
                {@const record = source.record(row.command_id) ?? row}
                <TableRow>
                  <TableCell colspan={6} class="whitespace-normal">
                    <CommandLifecycle
                      {record}
                      fault={source.recordError(row.command_id)}
                      resultText={resultText(record)}
                    />
                  </TableCell>
                </TableRow>
              {/if}
            {/each}
          </TableBody>
        </Table>
      {/if}
      {#if source.more}
        <Button
          variant="secondary"
          size="sm"
          class="self-start"
          disabled={source.loading}
          onclick={() => {
            void source.loadMore();
          }}
        >
          {t('printerDetailHistory.commands.more')}
        </Button>
      {/if}
    {/if}
  </CardContent>
</Card>
