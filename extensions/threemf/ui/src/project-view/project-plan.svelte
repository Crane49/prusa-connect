<!--
  The beds of a 3MF project drawn to scale from above, with every copy at its
  place, in the colour of the filament it prints with.

  The plan draws what the browser's view names: one bed alone and large,
  every bed at the offset the slicer gave it, or the copies on no bed.

  Pressing a copy ticks it, and marks its object in the list. A double press
  opens the object's panel. The pointer on a copy lights every copy of its
  object, here and in the list. Once anything is ticked, the copies that are
  not fade, so the selection reads at a glance.

  A copy left out of the print is grey and dashed, one partly off its bed has
  a warning outline, and one on no bed a destructive one, so a state never
  rests on colour alone: the tip and the label of each copy say it in words.

  The copies take one tab stop between them, and the arrow keys move from copy
  to copy in the project's order, so a project of a hundred copies is not a
  hundred tab stops. Enter ticks a copy and Escape lets go of the mark.
-->
<script lang="ts">
  import { t } from '../i18n';
  import { getProjectBrowser } from './browser.svelte';
  import { bedLabel, copyLabel, copySize, PLACEMENT_KEY } from './labels';
  import {
    bedKey,
    fittedLabel,
    objectColour,
    type PlanInstance,
    planOf,
    type Project,
  } from './model';
  import { legendSwatch, type LegendEntry } from './variants';

  interface Props {
    project: Project;
  }

  let { project }: Props = $props();

  const browser = getProjectBrowser();

  let hoveredKey = $state<string | undefined>(undefined);
  let focusedKey = $state<string | undefined>(undefined);

  const focus = $derived(browser.focus);
  const plan = $derived(planOf(project, focus));

  // The colour of each object, once per object rather than once per copy.
  const colours = $derived(
    project.objects.map((object) => objectColour(project, object)),
  );

  // A copy keeps the tab stop the reader last gave it, and the first copy
  // holds it before that.
  const tabStop = $derived(
    plan.instances.find((copy) => copy.key === focusedKey)?.key ??
      plan.instances[0]?.key,
  );

  // What the tip describes: the copy under the pointer, else the focused one.
  const shown = $derived(
    plan.instances.find((copy) => copy.key === (hoveredKey ?? focusedKey)),
  );
  const shownInstance = $derived(
    shown === undefined
      ? undefined
      : project.objects[shown.object]?.instances[shown.instance],
  );

  // The object a person is looking at: under the pointer, here or in the
  // list, else the one last pressed.
  const lit = $derived(browser.hovered ?? browser.active);
  const ticking = $derived(browser.selection.count > 0);

  // A copy's state as a person reads it: whether it prints, where it stands
  // when that is not whole on its bed, and whether it is ticked.
  const stateText = (copy: PlanInstance): string =>
    [
      t(
        copy.printable
          ? 'projectView.state.printable'
          : 'projectView.state.nonPrintable',
      ),
      copy.placement === 'inside' ? '' : t(PLACEMENT_KEY[copy.placement]),
      browser.isSelected(copy.object, copy.instance)
        ? t('projectView.plan.ticked')
        : '',
    ]
      .filter((part) => part !== '')
      .join(', ');

  const tipWhere = $derived(
    shown === undefined || shownInstance === undefined
      ? ''
      : [
          stateText(shown),
          shownInstance.bed === undefined || focus !== undefined
            ? ''
            : t('projectView.copy.where', {
                bed: bedLabel(project, shownInstance.bed),
              }),
        ]
          .filter((part) => part !== '')
          .join(', '),
  );

  const LEGEND_ORDER: readonly LegendEntry[] = [
    'ticked',
    'nonPrintable',
    'partlyOutside',
    'outside',
  ];

  const LEGEND_KEY: Readonly<Record<LegendEntry, string>> = {
    ticked: 'projectView.plan.ticked',
    nonPrintable: 'projectView.state.nonPrintable',
    partlyOutside: 'projectView.state.partlyOutside',
    outside: 'projectView.state.outside',
  };

  // The legend names only what the drawing holds that colour does not say.
  const legend = $derived(
    LEGEND_ORDER.filter((entry) => {
      if (entry === 'ticked') return ticking;

      return plan.instances.some((copy) => {
        if (entry === 'nonPrintable') return !copy.printable;
        if (entry === 'partlyOutside')
          return copy.placement === 'partlyOutside';

        return copy.placement === 'outside' || copy.offBed;
      });
    }),
  );

  // The size of a copy's label, a little under a bed's.
  const LABEL_SHARE = 0.8;
  const labelSize = $derived(plan.text * LABEL_SHARE);

  // A label is drawn only where it fits inside the copy's outline. The tip
  // carries the whole name.
  const labelOf = (copy: PlanInstance): string =>
    fittedLabel(project.objects[copy.object]?.name ?? '', copy, labelSize);

  // Where nothing is ticked, a press opens the copy's object.
  const press = (copy: PlanInstance): void => {
    browser.active = copy.object;
    if (browser.ticking) browser.toggle(copy.object, copy.instance);
    else browser.opened = copy.object;
  };

  const moveFocus = (from: PlanInstance, step: number, target: Element) => {
    const at = plan.instances.findIndex((copy) => copy.key === from.key);
    const next =
      plan.instances[
        (at + step + plan.instances.length) % plan.instances.length
      ];
    if (next === undefined) return;
    focusedKey = next.key;
    target
      .closest('svg')
      ?.querySelector<SVGGElement>(`[data-copy="${next.key}"]`)
      ?.focus();
  };

  const onCopyKey = (event: KeyboardEvent, copy: PlanInstance): void => {
    const target = event.currentTarget;
    if (!(target instanceof Element)) return;
    const index = plan.instances.indexOf(copy);
    switch (event.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        event.preventDefault();
        moveFocus(copy, 1, target);
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        event.preventDefault();
        moveFocus(copy, -1, target);
        break;
      case 'Home':
        event.preventDefault();
        moveFocus(copy, -index, target);
        break;
      case 'End':
        event.preventDefault();
        moveFocus(copy, plan.instances.length - 1 - index, target);
        break;
      case 'Enter':
      case ' ':
        event.preventDefault();
        press(copy);
        break;
      case 'Escape':
        if (browser.active !== undefined) {
          event.preventDefault();
          browser.active = undefined;
        }
        break;
    }
  };

  const planLabel = $derived.by(() => {
    if (focus === undefined) return t('projectView.plan.drawing');
    if (focus === 'off') return t('projectView.plan.offDrawing');

    return t('projectView.plan.bedLabel', { bed: bedLabel(project, focus) });
  });

  const unknownBed = $derived(plan.beds.some((bed) => !bed.known));
</script>

<div
  class="grid min-w-0 grid-cols-1 content-start gap-3"
  data-slot="project-plan"
>
  <div
    class="plan-frame relative mx-auto w-full"
    style="--plan-x: {plan.x}; --plan-y: {plan.y}; --plan-w: {plan.width}; --plan-h: {plan.height}"
  >
    <svg
      class="plan block size-full"
      viewBox="{plan.x} {plan.y} {plan.width} {plan.height}"
      role="group"
      aria-label={planLabel}
      preserveAspectRatio="xMidYMid meet"
      data-ticking={ticking}
    >
      {#each plan.beds as bed (bed.key)}
        {@const label = bedLabel(project, bed.ref)}
        {#if focus === undefined && plan.beds.length > 1}
          <g
            class="bed-button cursor-pointer outline-none"
            role="button"
            tabindex="0"
            aria-label={t('projectView.plan.open', { bed: label })}
            onclick={() => {
              browser.view = bed.key;
            }}
            onkeydown={(event) => {
              if (event.key !== 'Enter' && event.key !== ' ') return;
              event.preventDefault();
              browser.view = bedKey(bed.ref);
            }}
          >
            {@render bedShape(bed)}
          </g>
        {:else}
          {@render bedShape(bed)}
        {/if}
        {#if focus === undefined}
          <text
            class="fill-muted-foreground font-sans font-medium"
            x={bed.x}
            y={bed.y - plan.text * 0.5}
            font-size={plan.text}
          >
            {label}
          </text>
        {/if}
        {#if bed.empty}
          <text
            class="pointer-events-none fill-muted-foreground font-sans"
            x={bed.x + bed.width / 2}
            y={bed.y + bed.height / 2}
            font-size={plan.text}
            text-anchor="middle"
            dominant-baseline="central"
          >
            {t('projectView.plan.emptyBed')}
          </text>
        {:else if bed.parkedOnly}
          <text
            class="pointer-events-none fill-muted-foreground font-sans"
            x={bed.x + bed.width / 2}
            y={bed.y + plan.text * 1.5}
            font-size={plan.text * 0.8}
            text-anchor="middle"
            dominant-baseline="central"
          >
            {t('projectView.plan.parkedBed')}
          </text>
        {/if}
      {/each}

      {#if plan.offBed !== undefined}
        <rect
          class="off-bed fill-destructive/5 stroke-destructive stroke-1"
          x={plan.offBed.x}
          y={plan.offBed.y}
          width={plan.offBed.width}
          height={plan.offBed.height}
        />
        {#if focus === undefined}
          <text
            class="fill-destructive-text font-sans font-medium"
            x={plan.offBed.x}
            y={plan.offBed.y - plan.text * 0.5}
            font-size={plan.text}
          >
            {t('projectView.plan.offBed')}
          </text>
        {/if}
      {/if}

      {#each plan.instances as copy (copy.key)}
        {@const ticked = browser.isSelected(copy.object, copy.instance)}
        {@const colour = colours[copy.object]}
        <g
          class="copy cursor-pointer outline-none"
          data-copy={copy.key}
          data-state={copy.state}
          data-placement={copy.offBed ? 'outside' : copy.placement}
          data-printable={copy.printable}
          data-selected={ticked}
          data-lit={lit === copy.object}
          style={colour === undefined ? undefined : `--copy: ${colour}`}
          role="button"
          tabindex={copy.key === tabStop ? 0 : -1}
          aria-pressed={browser.ticking &&
          browser.canSelect(copy.object, copy.instance)
            ? ticked
            : undefined}
          aria-label={`${copyLabel(project, copy.object, copy.instance)}, ${stateText(copy)}`}
          onclick={() => {
            focusedKey = copy.key;
            press(copy);
          }}
          ondblclick={() => {
            browser.opened = copy.object;
          }}
          onkeydown={(event) => {
            onCopyKey(event, copy);
          }}
          onfocus={() => {
            focusedKey = copy.key;
          }}
          onblur={() => {
            if (focusedKey === copy.key) focusedKey = undefined;
          }}
          onpointerenter={() => {
            hoveredKey = copy.key;
            browser.hovered = copy.object;
          }}
          onpointerleave={() => {
            if (hoveredKey === copy.key) hoveredKey = undefined;
            if (browser.hovered === copy.object) browser.hovered = undefined;
          }}
        >
          <polygon
            class="copy-shape transition-opacity motion-reduce:transition-none"
            points={copy.points}
          />
        </g>
      {/each}

      <!-- The names sit in a layer of their own above the copies. Each copy
           is named by its own label already, and a name cut to fit would
           not match it. -->
      <g aria-hidden="true">
        {#each plan.instances as copy (copy.key)}
          {@const label = labelOf(copy)}
          {#if label !== ''}
            <text
              class="copy-name pointer-events-none fill-foreground font-sans"
              x={copy.centreX}
              y={copy.centreY}
              font-size={labelSize}
              text-anchor="middle"
              dominant-baseline="central"
            >
              {label}
            </text>
          {/if}
        {/each}
      </g>
    </svg>

    {#if shown !== undefined && shownInstance !== undefined}
      <div
        class="plan-tip pointer-events-none absolute z-10 grid max-w-64 grid-cols-1 content-start gap-0.5 rounded-sm border border-border bg-popover px-2 py-1.5 text-xs text-popover-foreground shadow-md"
        style="--tip-x: {shown.centreX}; --tip-y: {shown.centreY}"
        role="status"
      >
        <span class="font-medium break-words">
          {copyLabel(project, shown.object, shown.instance)}
        </span>
        <span class="text-muted-foreground">{copySize(shownInstance)}</span>
        <span class="text-muted-foreground">{tipWhere}</span>
        {#if shownInstance.outline_approx === true}
          <span class="text-muted-foreground">
            {t('projectView.copy.approx')}
          </span>
        {/if}
      </div>
    {/if}
  </div>

  <div
    class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground"
  >
    {#if legend.length > 0}
      <ul
        class="flex flex-wrap items-center gap-x-4 gap-y-2"
        aria-label={t('projectView.plan.legend')}
      >
        {#each legend as entry (entry)}
          <li class="inline-flex items-center gap-1.5">
            <span
              aria-hidden="true"
              class={['size-3 shrink-0 rounded-xs border', legendSwatch[entry]]}
            ></span>
            {t(LEGEND_KEY[entry])}
          </li>
        {/each}
      </ul>
    {/if}
    <p class="min-w-0 flex-1 basis-60">
      {#if unknownBed}
        {t('projectView.plan.unknownSize')}
      {:else if plan.instances.length === 0}
        {t('projectView.plan.empty')}
      {:else if browser.ticking}
        {t('projectView.plan.hint')}
      {:else}
        {t('projectView.plan.hintOpen')}
      {/if}
    </p>
  </div>
</div>

{#snippet bedShape(bed: (typeof plan.beds)[number])}
  {#if bed.shape !== undefined}
    <polygon
      class="bed fill-muted/60 stroke-border stroke-1"
      points={bed.shape}
    />
  {:else}
    <rect
      class={[
        'bed fill-muted/60 stroke-border stroke-1',
        bed.known ? '' : 'bed-unknown',
      ]}
      x={bed.x}
      y={bed.y}
      width={bed.width}
      height={bed.height}
    />
  {/if}
{/snippet}

<style>
  /* The frame takes the drawing's own proportions, and a tall drawing stops
     at most of the window's height rather than pushing the page down. */
  .plan-frame {
    aspect-ratio: var(--plan-w) / var(--plan-h);
    max-inline-size: calc(64svh * var(--plan-w) / var(--plan-h));
  }

  /* The tip stands above the copy it describes. Its place is the copy's
     centre as a share of the drawing, which the frame shares exactly. It
     slides from its left edge at the drawing's left to its right edge at the
     drawing's right, so it never leaves the frame sideways on a phone. */
  .plan-tip {
    --tip-share: calc((var(--tip-x) - var(--plan-x)) / var(--plan-w));

    inset-inline-start: calc(var(--tip-share) * 100%);
    inset-block-start: calc(
      (var(--tip-y) - var(--plan-y)) / var(--plan-h) * 100%
    );
    translate: calc(var(--tip-share) * -100%) calc(-100% - 0.5rem);
  }

  /* A stroke keeps its width in screen pixels at any zoom of the drawing. */
  .plan :global(polygon),
  .plan :global(rect) {
    vector-effect: non-scaling-stroke;
  }

  /* A copy is its filament's colour, or the muted ink for a file that names
     none. The outline is the same colour pulled toward the text colour, so a
     white filament still has an edge on a light bed. */
  .copy {
    --copy-ink: var(--copy, var(--muted-foreground));
  }

  .copy-shape {
    fill: color-mix(in oklch, var(--copy-ink) 70%, transparent);
    stroke: color-mix(in oklch, var(--copy-ink), var(--foreground) 40%);
    stroke-width: 1;
  }

  .copy[data-printable='false'] .copy-shape {
    fill: var(--muted);
    stroke: var(--muted-foreground);
  }

  .copy[data-placement='partlyOutside'] .copy-shape {
    stroke: var(--warning-text);
    stroke-width: 2;
  }

  .copy[data-placement='outside'] .copy-shape {
    stroke: var(--destructive);
    stroke-width: 2;
  }

  .bed-unknown,
  .off-bed,
  .copy[data-printable='false'] .copy-shape {
    stroke-dasharray: 4 3;
  }

  /* Once anything is ticked, what is not fades back. */
  .plan[data-ticking='true'] .copy[data-selected='false'] .copy-shape {
    opacity: 0.35;
  }

  .copy[data-selected='true'] .copy-shape {
    fill: color-mix(in oklch, var(--copy-ink) 90%, transparent);
    stroke: var(--primary);
    stroke-width: 3;
  }

  .copy[data-lit='true'] .copy-shape {
    opacity: 1;
    stroke: var(--foreground);
    stroke-width: 3;
  }

  .bed-button:focus-visible .bed,
  .copy:focus-visible .copy-shape {
    stroke: var(--ring);
    stroke-width: 3;
  }

  /* A name stays readable on any filament colour. */
  .copy-name {
    paint-order: stroke;
    stroke: color-mix(in oklch, var(--background) 80%, transparent);
    stroke-width: 2px;
    stroke-linejoin: round;
  }
</style>
