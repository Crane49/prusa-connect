<!--
  One notification: what happened, when, and what can be done about it.

  The row leads with the machine or the plate. An operator knows their printers
  by shape long before they have read a label, and a finished print is
  recognised by its plate, so `printerModel` and `thumbnailUrl` draw the
  leading box and the tone's glyph rides its corner. A row with neither keeps
  the tinted chip.

  Tone and read state are separate facts, and one row shows both. Tone is the
  chip and the word; read state is the dot beside the time and the title's
  weight. Neither rests on colour alone: the word is written out and the weight
  says it again (WCAG 1.4.1).

  What a row offers is a button, never a whole row link. Making the row an
  anchor would put buttons inside a link, which is invalid and reaches the
  keyboard in an order nobody expects.
-->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import CirclePlusIcon from '@lucide/svelte/icons/circle-plus';
  import MessageCircleQuestionIcon from '@lucide/svelte/icons/message-circle-question-mark';
  import ShieldXIcon from '@lucide/svelte/icons/shield-x';
  import PowerOffIcon from '@lucide/svelte/icons/power-off';
  import OctagonAlertIcon from '@lucide/svelte/icons/octagon-alert';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import UploadIcon from '@lucide/svelte/icons/upload';
  import WifiIcon from '@lucide/svelte/icons/wifi';
  import WifiOffIcon from '@lucide/svelte/icons/wifi-off';
  import type { Component } from 'svelte';
  import type { Snippet } from 'svelte';

  import { type NotificationKind, toneOf } from '../../domain';
  import { t } from '../../i18n';
  import Button from '../button/button.svelte';
  import PrinterIcon from '../printer-icon/printer-icon.svelte';
  import type { NotificationItem } from './types';
  import {
    TONE_CHIP,
    TONE_CHIP_BASE,
    TONE_CORNER,
    TONE_CORNER_BASE,
    TONE_DOT,
    TONE_DOT_BASE,
    TONE_LABEL,
  } from './variants';

  // One glyph per kind, so two rows of the same tone still look like different
  // events. A printer asking a question and a registration waiting for
  // approval are both `attention`, and one warning triangle for each said only
  // that something orange had happened.
  const KIND_ICONS: Record<NotificationKind, Component> = {
    'printer-attention': MessageCircleQuestionIcon,
    'printer-error': OctagonAlertIcon,
    'printer-offline': WifiOffIcon,
    'printer-connected': WifiIcon,
    'printer-pending': CirclePlusIcon,
    'printer-refused': ShieldXIcon,
    'job-finished': CircleCheckIcon,
    'job-failed': TriangleAlertIcon,
    'transfer-failed': UploadIcon,
    'module-switched-off': PowerOffIcon,
  };

  let {
    item,
    onAction,
    toggle,
  }: {
    item: NotificationItem;
    onAction?: ((notificationId: string, actionId: string) => void) | undefined;
    /** The read toggle, drawn by the panel so one shape serves every row. */
    toggle?: Snippet<[NotificationItem]> | undefined;
  } = $props();

  const tone = $derived(toneOf(item.kind));
  const Icon = $derived(KIND_ICONS[item.kind]);
  const hasArt = $derived(
    item.thumbnailUrl !== undefined || item.printerModel !== undefined,
  );
  const toneLead = $derived(
    t('primitives.notifications.toneLead', { tone: t(TONE_LABEL[tone]) }),
  );
</script>

<li
  data-slot="notification"
  data-kind={item.kind}
  data-tone={tone}
  data-read={item.read}
  class="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-accent"
>
  {#if hasArt}
    <!-- The machine or the plate, with the tone's glyph over its corner: art
         alone says which printer, never how bad. `bg-surface` is the box a
         file row gives a thumbnail, one step below the panel. -->
    <span
      aria-hidden="true"
      class="relative flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-surface"
    >
      {#if item.thumbnailUrl === undefined}
        <!-- `p-1`: the printer artwork is drawn to its own edges, and a 40px
             box with none left the machine touching the glyph. -->
        <PrinterIcon
          model={item.printerModel ?? 'unknown'}
          class="size-full p-1"
        />
      {:else}
        <img src={item.thumbnailUrl} alt="" class="size-full object-cover" />
      {/if}
      <span class={[TONE_CORNER_BASE, TONE_CORNER[tone]]}>
        <Icon class="size-3" />
      </span>
    </span>
  {:else}
    <span aria-hidden="true" class={[TONE_CHIP_BASE, TONE_CHIP[tone]]}>
      <Icon class="size-4" />
    </span>
  {/if}
  <div class="min-w-0 flex-1">
    <!-- The title, the time and the unread dot share one line. The tone
         survives in the glyph, which is a different shape per kind and not
         only a different colour, and in the title, which states the fact in
         words. `sr-only` keeps the tone's own word in the accessibility tree,
         where it costs no room at all. -->
    <div class="flex items-start gap-2">
      <p
        class={[
          'min-w-0 flex-1 text-sm leading-snug',
          item.read
            ? 'font-normal text-muted-foreground'
            : 'font-medium text-foreground',
        ]}
      >
        <span class="sr-only">{toneLead}</span>
        {item.title}
      </p>
      <span class="flex shrink-0 items-center gap-1.5">
        <span
          class="pt-0.5 font-mono text-xs text-muted-foreground tabular-nums"
        >
          {item.at}
        </span>
        {#if !item.read}
          <span
            aria-hidden="true"
            class={[TONE_DOT_BASE, TONE_DOT[tone], 'mt-1']}
          ></span>
        {/if}
        {@render toggle?.(item)}
      </span>
    </div>
    {#if item.body}
      <p class="mt-1 text-xs leading-snug text-muted-foreground">{item.body}</p>
    {/if}
    <!-- What this notification offers, on its own line and only where there is
         something to offer. `sm` and `secondary` is what a row in a 26rem
         panel has room for. -->
    {#if item.actions && item.actions.length > 0}
      <div class="mt-2 flex flex-wrap items-center gap-1.5">
        {#each item.actions as action (action.id)}
          {#if action.href === undefined}
            <Button
              size="sm"
              variant={action.variant ?? 'secondary'}
              onclick={() => onAction?.(item.id, action.id)}
            >
              {action.label}
            </Button>
          {:else}
            <!-- An action that goes somewhere is a link drawn as the button,
                 so it opens in a new tab like any other link. -->
            <Button size="sm" variant={action.variant ?? 'secondary'}>
              {#snippet child({ props })}
                <a
                  {...props}
                  href={action.href}
                  onclick={() => onAction?.(item.id, action.id)}
                >
                  {action.label}
                </a>
              {/snippet}
            </Button>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</li>
