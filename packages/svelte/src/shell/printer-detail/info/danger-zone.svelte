<!-- The one act on this tab that cannot be taken back. -->
<script lang="ts">
  import { describeFault, t } from '../../../i18n';
  import {
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    ConfirmDialog,
    FaultAlert,
  } from '../../../primitives';
  import type { PrinterAdministration } from './types';

  let {
    name,
    administration,
    removable,
    onRemoved,
  }: {
    name: string;
    administration: PrinterAdministration;
    /** False for a printer that runs this server: the zone says why it offers no removal. */
    removable: boolean;
    onRemoved?: (() => void) | undefined;
  } = $props();

  const pending = $derived(administration.pending('retire'));
  const fault = $derived(
    administration.failed === 'retire' && administration.error !== undefined
      ? describeFault(administration.error)
      : undefined,
  );

  // The page moves on only once the server has retired the printer.
  const remove = async (): Promise<void> => {
    if (await administration.retire()) onRemoved?.();
  };
</script>

{#snippet removeTrigger({ props }: { props: Record<string, unknown> })}
  <Button
    {...props}
    variant="destructive"
    class="self-start"
    disabled={pending}
    aria-busy={pending}
  >
    {t('printerDetailInfo.danger.action')}
  </Button>
{/snippet}

<Card class="border-s-4 border-s-state-fault-edge">
  <CardHeader>
    <CardTitle
      ><h2 class="contents">
        {t('printerDetailInfo.danger.heading')}
      </h2></CardTitle
    >
  </CardHeader>
  <CardContent class="flex flex-col items-start gap-4">
    {#if removable}
      <p class="text-sm text-muted-foreground">
        {t('printerDetailInfo.danger.explain', { name })}
      </p>
      <FaultAlert {fault} />
      <!-- The title names the printer. -->
      <ConfirmDialog
        trigger={removeTrigger}
        title={t('printerDetailInfo.danger.title', { name })}
        description={t('printerDetailInfo.danger.detail', { name })}
        confirmLabel={t('printerDetailInfo.danger.confirm')}
        onConfirm={() => {
          void remove();
        }}
      />
    {:else}
      <p class="text-sm text-muted-foreground">
        {t('printerDetailInfo.danger.resident', { name })}
      </p>
    {/if}
  </CardContent>
</Card>
