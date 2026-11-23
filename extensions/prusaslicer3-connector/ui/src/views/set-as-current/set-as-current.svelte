<!--
  "Set as current", the action Prusa Connect shows on a printer when
  PrusaSlicer shows the page.
  It asks the slicer to select this printer, and the slicer opens its
  Slicing tab. Outside PrusaSlicer it draws nothing.

  `row` is the icon button at the end of each printer in the listing, the
  cloud's `iconOnly`. The host lifts it above the row's link. `header` is the
  button under the printer page's header, above its tabs.
-->
<script lang="ts">
  import CircleDotIcon from '@lucide/svelte/icons/circle-dot';
  import { Button } from '@prusa/connect-ui/primitives';
  import { type Slicer, slicer as pageSlicer } from '@prusa/connect-ui/slicer';

  import type { Translate } from '../../api';

  let {
    deviceId,
    t,
    look,
    slicer = pageSlicer,
  }: {
    /** The printer's id, which the module's printer list answers as `uuid`. */
    deviceId: string;
    t: Translate;
    look: 'row' | 'header';
    /** The page's own slicer helper. A test hands in a fake. */
    slicer?: Slicer;
  } = $props();

  const select = (): void => {
    slicer.post({ action: 'SELECT_PRINTER', uuid: deviceId });
  };
</script>

{#if slicer.available}
  {#if look === 'row'}
    <Button
      variant="ghost"
      size="icon-sm"
      label={t('printer.setAsCurrent')}
      data-part="set-as-current"
      onclick={select}
    >
      <CircleDotIcon aria-hidden="true" />
    </Button>
  {:else}
    <div data-part="set-as-current">
      <Button variant="secondary" size="md" onclick={select}>
        <CircleDotIcon aria-hidden="true" />
        {t('printer.setAsCurrent')}
      </Button>
    </div>
  {/if}
{/if}
