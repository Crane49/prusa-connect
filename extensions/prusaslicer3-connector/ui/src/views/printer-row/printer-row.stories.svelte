<!--
  One printer in the print dialog, in each way it can stand: ready, ready
  with a problem, and unable to print now.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import { RadioGroup } from '@prusa/connect-ui/primitives';

  import { candidatesFor } from '../../compatibility';
  import { farm, mk4sRequest, t } from '../../test/fakes';
  import PrinterRow from './printer-row.svelte';

  const [ready, mismatched, busy, offline] = candidatesFor(mk4sRequest, farm);

  const { Story } = defineMeta({
    title: 'PrusaSlicer 3/PrinterRow',
    component: PrinterRow,
  });
</script>

{#each [{ name: 'Ready', candidate: ready }, { name: 'Different nozzle and material', candidate: mismatched }, { name: 'Printing', candidate: busy }, { name: 'Offline', candidate: offline }] as row (row.name)}
  {#if row.candidate}
    <Story name={row.name} asChild>
      <RadioGroup value="mk4s-1" aria-label={t('select.printers')}>
        <PrinterRow candidate={row.candidate} {t} />
      </RadioGroup>
    </Story>
  {/if}
{/each}
