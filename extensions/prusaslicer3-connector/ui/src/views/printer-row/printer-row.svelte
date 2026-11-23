<!--
  One printer in the print dialog: its name, model, nozzles and state, and
  every way it does not suit the file. A printer that cannot start a print
  now is drawn and cannot be picked. It sits inside the dialog's RadioGroup.
-->
<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import { Badge, Label, RadioGroupItem } from '@prusa/connect-ui/primitives';

  import type { Translate } from '../../api';
  import { type Candidate, modelOf, toolsOf } from '../../compatibility';
  import { STATE_VARIANTS } from './variants';

  let { candidate, t }: { candidate: Candidate; t: Translate } = $props();

  const printer = $derived(candidate.printer);
  const id = $derived(`printer-${printer.uuid}`);
  const nozzles = $derived(
    toolsOf(printer)
      .map((tool) => tool.nozzleDiameter)
      .filter((diameter) => diameter !== undefined)
      .map((diameter) => formatNumber(diameter))
      .join(' / '),
  );
  const variant = $derived(
    STATE_VARIANTS[printer.connect_state] ?? 'stateQuiet',
  );
</script>

<div
  data-part="printer-row"
  data-printable={candidate.printable}
  class="flex items-start gap-3 border-b border-border p-3 last:border-b-0"
>
  <RadioGroupItem
    value={printer.uuid}
    {id}
    disabled={!candidate.printable}
    aria-describedby={`${id}-state`}
  />
  <Label for={id} variant="option" class="grid min-w-0 flex-1 gap-1">
    <span class="font-medium">{printer.name ?? t('select.unnamed')}</span>
    <span class="text-xs text-muted-foreground">
      {modelOf(printer) ?? ''}
      {#if nozzles !== ''}
        · {t('select.nozzle', { diameter: nozzles })}
      {/if}
    </span>
    {#each candidate.problems as problem (problem)}
      <span data-part="printer-problem" class="text-xs text-warning-text">
        {t(`select.problem.${problem}`)}
      </span>
    {/each}
  </Label>
  <span id={`${id}-state`} class="grid justify-items-end gap-1">
    <Badge {variant}>{t(`state.${printer.connect_state}`)}</Badge>
    {#if !candidate.printable}
      <span class="text-xs text-muted-foreground">
        {t('select.notPrintable')}
      </span>
    {/if}
  </span>
</div>
