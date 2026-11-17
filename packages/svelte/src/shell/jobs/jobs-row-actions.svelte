<!-- What one print's row can do. -->
<script lang="ts">
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';

  import { t } from '../../i18n';
  import { DropdownMenuItem, RowActions } from '../../primitives';
  import type { JobRow } from './types';

  let {
    row,
    printAgainOf,
  }: {
    row: JobRow;
    /** The Print again press for one row, or undefined where the row offers none. */
    printAgainOf?: ((row: JobRow) => (() => void) | undefined) | undefined;
  } = $props();

  // The print dialog opened by the press decides whether the printer can take
  // the file, so the item itself is never drawn disabled.
  const printAgain = $derived(printAgainOf?.(row));
</script>

<RowActions name={row.fileName}>
  {#if printAgain}
    <DropdownMenuItem
      onSelect={() => {
        printAgain();
      }}
    >
      <RotateCcwIcon aria-hidden="true" />
      {t('jobs.action.printAgain')}
    </DropdownMenuItem>
  {/if}
  <DropdownMenuItem>
    {#snippet child({ props })}
      <a {...props} href={row.printerHref}>
        <ExternalLinkIcon aria-hidden="true" />
        {t('jobs.action.openPrinter')}
      </a>
    {/snippet}
  </DropdownMenuItem>
</RowActions>
