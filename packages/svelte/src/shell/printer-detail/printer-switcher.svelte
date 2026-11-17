<!--
  Which of two printers this screen is about.

  A `group` and not a `tablist`, deliberately: a tab controls a panel, and this
  control has none — it changes the subject of the whole screen. Built as tabs
  it pointed `aria-controls` at a panel that does not exist, which is an
  invalid ARIA value and an axe failure.

  Half-width is structural. Switching exists when the instance owns exactly two
  printers and nowhere else, so `grid-cols-2` divides the row without measuring
  anything and the chosen half's underline spans its own half.

  The halves keep the fleet's own order, so the second printer is still the
  second half. Drawing the shown printer first would move the control under the
  pointer to the other end of the row on every switch.
-->
<script lang="ts">
  import {
    PRINTER_STATE_LABEL_KEY,
    PRINTER_STATE_SEVERITY_TIER,
    type PrinterSeverityTier,
  } from '../../domain';
  import { t } from '../../i18n';
  import { Badge, PrinterIcon } from '../../primitives';
  import { printerRowOf, stateBadgeVariant } from '../printers/presentation';
  import type { PrinterSwitcherProps } from './types';

  let { printers, chosenId, onChoose }: PrinterSwitcherProps = $props();

  const halves = $derived(printers.map((device) => printerRowOf(device)));

  /**
   * The dot a half carries where the badge has no room for its word. Undefined
   * for a printer simply working: two dots that always show mean nothing, and
   * one that shows only when a machine wants a person means everything.
   * Colour is never the only carrier — the half states the label as `sr-only`
   * text either way.
   */
  const dotClass = (tier: PrinterSeverityTier): string | undefined => {
    if (tier === 'critical') return 'bg-destructive';
    if (tier === 'attention') return 'bg-warning';
    if (tier === 'not-reporting') return 'bg-muted-foreground';
    return undefined;
  };
</script>

<div
  role="group"
  aria-label={t('printerDetail.switcher.label')}
  class="@container/printer-switcher grid w-full grid-cols-2"
>
  {#each halves as half, index (half.id)}
    {@const chosen = half.id === chosenId}
    {@const dot = dotClass(PRINTER_STATE_SEVERITY_TIER[half.state])}
    <!-- eslint-disable-next-line svelte/no-restricted-html-elements -- standing exception (ui-integration.md): the two-up head switcher draws a printer picture and an underline that neither ToggleGroup nor Tabs does -->
    <button
      type="button"
      aria-pressed={chosen}
      onclick={chosen ? undefined : () => onChoose(half.id)}
      class={[
        'flex w-full min-w-0 items-center gap-2 border-b-2 px-3 py-3 text-sm font-semibold transition-colors outline-none',
        'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        '@lg/printer-switcher:gap-3 @lg/printer-switcher:px-4 @lg/printer-switcher:text-base',
        chosen
          ? 'border-b-selected text-foreground'
          : 'border-b-border text-muted-foreground hover:text-foreground',
        // The halves report their state in the same column, so without a rule
        // between them a right-aligned badge reads as belonging to whichever
        // name it happens to sit beside.
        index > 0 && 'border-s border-s-border',
      ]}
    >
      <!-- Decoration here: the button's own words name the printer. -->
      <PrinterIcon
        aria-hidden="true"
        model={half.artwork}
        reported={half.model}
        class="size-6 shrink-0 @lg/printer-switcher:size-8"
      />
      <span class="min-w-0 truncate" title={half.name}>{half.name}</span>
      <span class="sr-only">{t(PRINTER_STATE_LABEL_KEY[half.state])}</span>
      {#if dot !== undefined}
        <span
          aria-hidden="true"
          class={[
            'ms-auto size-2.5 shrink-0 rounded-full @lg/printer-switcher:hidden',
            dot,
          ]}
        ></span>
      {/if}
      <Badge
        size="md"
        aria-hidden="true"
        variant={stateBadgeVariant(half.state)}
        class="ms-auto hidden @lg/printer-switcher:inline-flex"
      >
        {t(PRINTER_STATE_LABEL_KEY[half.state])}
      </Badge>
    </button>
  {/each}
</div>
