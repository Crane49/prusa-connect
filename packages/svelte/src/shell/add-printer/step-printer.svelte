<!--
  Step two: what a person does at the printer, in the words its display uses.
  The menu to open is drawn as the path of items the display shows, and each
  numbered item is one action, so a person can glance back from the printer and
  find their place.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

  import { t } from '../../i18n';
  import NumberedStep from './numbered-step.svelte';
  import type { PrinterAdmission } from './types';

  let { admission }: { admission: PrinterAdmission } = $props();

  // The names of the menu items on the printer's display, in the order a
  // person opens them.
  const menu = $derived([
    t('addPrinter.printer.menu.settings'),
    t('addPrinter.printer.menu.network'),
    t('addPrinter.printer.menu.connect'),
  ]);
</script>

<div class="flex flex-col gap-6" data-part="step-printer">
  <ol class="flex flex-col gap-4">
    <NumberedStep number={1}>
      <p>{t('addPrinter.printer.open')}</p>
      <ol
        aria-label={t('addPrinter.printer.menu')}
        class="flex flex-wrap items-center gap-1.5"
        data-part="menu-path"
      >
        {#each menu as item, index (item)}
          <li class="flex items-center gap-1.5">
            <span
              class="rounded-sm border border-border bg-muted px-2 py-0.5 text-xs font-medium text-foreground"
            >
              {item}
            </span>
            {#if index < menu.length - 1}
              <ChevronRightIcon
                aria-hidden="true"
                class="size-3.5 text-muted-foreground rtl:rotate-180"
              />
            {/if}
          </li>
        {/each}
      </ol>
    </NumberedStep>
    <NumberedStep number={2}>
      <p>{t('addPrinter.printer.load')}</p>
      <p class="text-muted-foreground" data-part="no-config">
        {t('addPrinter.printer.noConfig')}
      </p>
    </NumberedStep>
    <NumberedStep number={3}>
      <p>{t('addPrinter.printer.add')}</p>
    </NumberedStep>
    <NumberedStep number={4}>
      <p>
        {admission === 'automatic'
          ? t('addPrinter.printer.code.automatic')
          : t('addPrinter.printer.code.approval')}
      </p>
    </NumberedStep>
  </ol>
  <p class="text-sm text-muted-foreground">{t('addPrinter.printer.note')}</p>
</div>
