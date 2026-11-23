<!--
  The print dialog PrusaSlicer opens at /m/<namespace>/select-printer?embed=1.
  The file name, the printers that take the file, and Print. The slicer
  uploads the file itself once it hears which printer, so this page sends no
  request of its own beyond reading the printers.

  The only plan is "print now": this server has no queue.
-->
<script lang="ts">
  import {
    Button,
    EmptyState,
    FaultAlert,
    Field,
    Input,
    PageHeader,
    RadioGroup,
    RowList,
    Spinner,
  } from '@prusa/connect-ui/primitives';

  import type { Translate } from '../../api';
  import type { PickerView } from '../../select-printer.svelte';
  import { PrinterRow } from '../printer-row';

  let { view, t }: { view: PickerView; t: Translate } = $props();

  const filenameMissing = $derived(
    view.request !== undefined &&
      view.request.multipleBeds !== true &&
      view.filename.trim() === '',
  );
  const hint = $derived.by(() => {
    if (filenameMissing) return t('select.filenameMissing');
    if (view.selected === undefined) return t('select.noPrinter');

    return undefined;
  });
</script>

<section data-part="select-printer" class="mx-auto grid max-w-2xl gap-6">
  <PageHeader title={t('select.title')} />

  {#if !view.inSlicer}
    <EmptyState
      headingLevel="h2"
      heading={t('select.outside.heading')}
      description={t('select.outside.note')}
    />
  {:else if view.sent}
    <p role="status" class="text-sm">{t('select.sent')}</p>
  {:else if view.load === 'loading' || view.request === undefined}
    <p
      role="status"
      aria-busy="true"
      class="flex items-center gap-2 text-sm text-muted-foreground"
    >
      <Spinner />
      {view.load === 'loading' ? t('select.loading') : t('select.waiting')}
    </p>
  {:else if view.load === 'failed' || view.load === 'refused'}
    <FaultAlert fault={view.fault}>
      {#snippet actions()}
        {#if view.load === 'failed'}
          <Button variant="secondary" onclick={() => void view.refresh()}>
            {t('select.retry')}
          </Button>
        {/if}
      {/snippet}
    </FaultAlert>
  {:else}
    {#if view.request.multipleBeds === true}
      <p data-part="multiple-beds" class="text-sm">
        {t('select.multipleBeds')}
      </p>
    {:else}
      <Field
        label={t('select.filename')}
        control={Input}
        {...filenameMissing ? { error: t('select.filenameMissing') } : {}}
        controlProps={{
          name: 'filename',
          autocomplete: 'off',
          value: view.filename,
          oninput: (event: Event) => {
            if (event.currentTarget instanceof HTMLInputElement) {
              view.filename = event.currentTarget.value;
            }
          },
        }}
      />
    {/if}

    <div class="grid gap-2">
      <h2 class="text-sm font-semibold">{t('select.printers')}</h2>
      {#if view.candidates.length === 0}
        <EmptyState
          size="sm"
          headingLevel="h3"
          heading={t(
            view.noPrinters ? 'select.none.heading' : 'select.empty.heading',
          )}
          description={t(
            view.noPrinters ? 'select.none.note' : 'select.empty.note',
          )}
        />
      {:else}
        <RadioGroup
          value={view.selected ?? ''}
          aria-label={t('select.printers')}
          onValueChange={(uuid: string) => {
            view.select(uuid);
          }}
        >
          <RowList>
            {#each view.candidates as candidate (candidate.printer.uuid)}
              <PrinterRow {candidate} {t} />
            {/each}
          </RowList>
        </RadioGroup>
      {/if}
    </div>
  {/if}

  {#if view.inSlicer && !view.sent}
    <div class="flex items-center justify-end gap-2">
      {#if hint !== undefined && view.load === 'ready' && view.request !== undefined}
        <span data-part="print-hint" class="text-xs text-muted-foreground">
          {hint}
        </span>
      {/if}
      <Button
        variant="secondary"
        onclick={() => {
          view.cancel();
        }}
      >
        {t('select.cancel')}
      </Button>
      <Button
        disabled={!view.ready}
        onclick={() => {
          view.print();
        }}
      >
        {t('select.print')}
      </Button>
    </div>
  {/if}
</section>
