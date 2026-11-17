<!--
  One command from request to outcome. Who authorized it reaches only a
  reader who may see the printer's audit, so for anybody else those rows say
  the value is not available.
-->
<script lang="ts">
  import type { CommandRecord, DataFault } from '../../../data';
  import { describeFault, formatDateTime, t } from '../../../i18n';
  import {
    FaultAlert,
    KeyValueList,
    type KeyValueRow,
  } from '../../../primitives';

  let {
    record,
    fault,
    resultText,
  }: {
    record: CommandRecord;
    /** Why the command's own read was refused. The listed row is drawn under it. */
    fault?: DataFault | undefined;
    resultText: string;
  } = $props();

  const at = (value: string | undefined): string | undefined =>
    value === undefined ? undefined : formatDateTime(value);

  // The principal as the server names one elsewhere: type and id.
  const authorizedBy = $derived(
    record.auth_id === undefined
      ? undefined
      : [record.auth_type, record.auth_id]
          .filter((part) => part !== undefined)
          .join(':'),
  );

  const rows = $derived<KeyValueRow[]>([
    {
      id: 'id',
      label: t('printerDetailHistory.commands.detail.id'),
      value: record.command_id,
      mono: true,
      copyable: true,
    },
    {
      id: 'requested',
      label: t('printerDetailHistory.commands.detail.requested'),
      value: at(record.requested_at),
    },
    {
      id: 'authorized',
      label: t('printerDetailHistory.commands.detail.authorized'),
      value: at(record.authorized_at),
    },
    {
      id: 'by',
      label: t('printerDetailHistory.commands.detail.by'),
      value: authorizedBy,
      mono: true,
    },
    {
      id: 'settled',
      label: t('printerDetailHistory.commands.detail.settled'),
      value: at(record.settled_at),
    },
    {
      id: 'result',
      label: t('printerDetailHistory.commands.detail.result'),
      value: resultText,
    },
    ...(record.fault_code === undefined
      ? []
      : [
          {
            id: 'fault',
            label: t('printerDetailHistory.commands.detail.fault'),
            value: record.fault_code,
            mono: true,
          },
        ]),
    ...(record.machine_reason === undefined
      ? []
      : [
          {
            id: 'reason',
            label: t('printerDetailHistory.commands.detail.reason'),
            value: record.machine_reason,
          },
        ]),
  ]);
</script>

<div class="flex flex-col gap-3 py-2">
  {#if fault}
    <FaultAlert fault={describeFault(fault)} />
  {/if}
  <KeyValueList
    {rows}
    absentText={t('printerDetailHistory.commands.detail.absent')}
  />
</div>
