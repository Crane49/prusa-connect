<!--
  The objects the plan's view shows, one row each, with a box to tick their
  copies there. The box in the head ticks every copy the view shows. The
  name is a button that opens the object's panel, named by the name alone.

  A copy that stands partly or wholly off its bed cannot be printed, so it
  cannot be ticked: its row says where it stands instead. The name opens the
  object's panel, with its parts and a picture of it.

  The pointer on a row lights the object's copies on the plan, and a copy
  pressed on the plan marks its row and scrolls it into sight.
-->
<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

  import { formatNumber, t } from '../i18n';
  import { Button, SelectAllCheckbox } from '@prusa/connect-ui/primitives';
  import { getProjectBrowser } from './browser.svelte';
  import ExtruderChips from './extruder-chips.svelte';
  import { objectName } from './labels';
  import { byObject, filamentsFor, objectColour, type Project } from './model';

  interface Props {
    project: Project;
  }

  let { project }: Props = $props();

  const browser = getProjectBrowser();

  let list = $state<HTMLUListElement | null>(null);

  // A project of one extruder says nothing a row needs to say about it.
  const manyTools = $derived((project.extruders ?? []).length > 1);

  const rows = $derived.by(() => {
    return byObject(browser.copies).map(({ object: index, copies }) => {
      const object = project.objects[index];
      const instances = copies.flatMap((copy) => {
        const at = object?.instances[copy.instance];

        return at === undefined ? [] : [at];
      });
      const selectable = copies.filter((copy) =>
        browser.canSelect(copy.object, copy.instance),
      ).length;
      const parked = instances.filter((at) => !at.printable).length;
      let note = '';
      if (selectable === 0) note = t('projectView.list.offBed');
      else if (parked === instances.length)
        note = t('projectView.state.nonPrintable');

      return {
        index,
        name: objectName(object?.name),
        colour:
          object === undefined ? undefined : objectColour(project, object),
        extruders: object?.extruders ?? [],
        filaments: filamentsFor(project, object),
        copies,
        selectable,
        note,
      };
    });
  });

  // A copy pressed on the plan brings its row into sight. This reaches the
  // DOM, which is what an effect is for.
  $effect(() => {
    const active = browser.active;
    if (active === undefined || list === null) return;
    list
      .querySelector(`[data-object="${String(active)}"]`)
      ?.scrollIntoView({ block: 'nearest' });
  });
</script>

<div
  class="grid min-h-0 min-w-0 grid-cols-1 content-start"
  data-slot="project-object-list"
>
  {#if rows.length === 0}
    <p class="py-6 text-center text-sm text-muted-foreground">
      {t('projectView.list.empty')}
    </p>
  {:else}
    <div
      class="flex items-center gap-3 border-b border-border px-2 pbe-2 text-xs text-muted-foreground"
    >
      {#if browser.ticking}
        <SelectAllCheckbox
          selection={browser.selection}
          keys={browser.keysOf(browser.copies)}
          label={t('projectView.list.selectAll')}
        />
      {/if}
      <span class="font-medium">
        {t('projectView.list.count', { count: rows.length })}
      </span>
    </div>
    <ul
      bind:this={list}
      class="list-scroll grid grid-cols-1 content-start overflow-y-auto py-1"
    >
      {#each rows as row (row.index)}
        {@const lit =
          browser.hovered === row.index || browser.active === row.index}
        <li
          data-object={row.index}
          data-lit={lit}
          class={[
            'flex min-w-0 items-start gap-3 rounded-sm px-2 py-1.5 transition-colors motion-reduce:transition-none',
            lit ? 'bg-accent' : '',
          ]}
          onpointerenter={() => {
            browser.hovered = row.index;
          }}
          onpointerleave={() => {
            if (browser.hovered === row.index) browser.hovered = undefined;
          }}
        >
          {#if browser.ticking}
            <SelectAllCheckbox
              selection={browser.selection}
              keys={browser.keysOf(row.copies)}
              class="mbs-0.5"
              label={t('projectView.list.select', { name: row.name })}
            />
          {/if}
          <span
            aria-hidden="true"
            class="swatch mbs-1 size-3 shrink-0 rounded-xs border border-border"
            style={row.colour === undefined
              ? undefined
              : `--swatch: ${row.colour}`}
          ></span>
          <div
            class="grid min-w-0 flex-1 grid-cols-1 content-start gap-1 text-sm"
          >
            <Button
              variant="link"
              size="inline-icon"
              class="min-w-0"
              onclick={() => {
                browser.opened = row.index;
              }}
            >
              <span class="min-w-0 truncate" title={row.name}>
                {row.name}
              </span>
              {#if row.copies.length > 1}
                <span
                  class="shrink-0 font-mono text-xs text-muted-foreground tabular-nums"
                  title={t('projectView.bed.copies', {
                    count: row.copies.length,
                  })}
                >
                  {t('projectView.beds.times', {
                    copies: formatNumber(row.copies.length),
                  })}
                </span>
              {/if}
              <ChevronRightIcon
                aria-hidden="true"
                class="size-4 shrink-0 text-muted-foreground"
              />
            </Button>
            {#if row.note !== ''}
              <span class="text-xs text-warning-text">{row.note}</span>
            {/if}
            {#if manyTools && row.extruders.length > 0}
              <ExtruderChips
                extruders={row.extruders}
                filaments={row.filaments}
                label={t('projectView.objects.extruders', { name: row.name })}
              />
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  /* The swatch is the object's filament, or the muted ink of the plan. */
  .swatch {
    background: var(--swatch, var(--muted-foreground));
  }

  /* Beside the plan, the list is no taller than the plan and scrolls. Under
     it, on a narrow screen, the page scrolls instead. */
  @container browser (width >= 56rem) {
    .list-scroll {
      max-block-size: 64svh;
    }
  }
</style>
