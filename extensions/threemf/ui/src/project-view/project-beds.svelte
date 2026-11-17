<!--
  The technical side of each bed of a 3MF project, as a card: its name, its
  size, the picture the slicer saved of it, how many copies stand on it,
  whether it prints object by object, the G-code the slicer stored for it,
  and the changes the slicer inserts at given heights.

  What stands on a bed is the plan's to show; this is the detail a person
  opens when a print needs checking.
-->
<script lang="ts">
  import FileCodeIcon from '@lucide/svelte/icons/file-code';
  import ListOrderedIcon from '@lucide/svelte/icons/list-ordered';
  import LockIcon from '@lucide/svelte/icons/lock';

  import { t } from '../i18n';
  import { Badge } from '@prusa/connect-ui/primitives';
  import { bedLabel, millimetres } from './labels';
  import ProjectLayerChanges from './project-layer-changes.svelte';
  import { type BedRef, bedKey, type Project } from './model';

  interface Props {
    project: Project;
    /** Where the picture at an index of the project's thumbnails is served. */
    thumbnailUrl?: ((index: number) => string) | undefined;
    /** One bed alone, such as the bed the plan shows. Absent is every bed. */
    only?: BedRef | undefined;
  }

  let { project, thumbnailUrl, only }: Props = $props();

  const cards = $derived(
    project.setups.flatMap((setup, s) =>
      setup.beds.flatMap((bed, b) => {
        if (only !== undefined && (only.setup !== s || only.bed !== b)) {
          return [];
        }

        const parked =
          bed.instances.length > 0 &&
          bed.instances.every(
            (at) =>
              project.objects[at.object]?.instances[at.instance]?.printable ===
              false,
          );
        const gcode = (bed.gcode ?? '') !== '';
        let note = '';
        if (bed.instances.length === 0)
          note = gcode
            ? t('projectView.beds.gcodeOnly')
            : t('projectView.beds.empty');
        else if (parked) note = t('projectView.beds.parked');

        return {
          key: bedKey({ setup: s, bed: b }),
          label: bedLabel(project, { setup: s, bed: b }),
          facts: [
            project.setups.length > 1 ? undefined : setup.printer_model,
            bed.size.x > 0 && bed.size.y > 0
              ? t('projectView.bed.size', {
                  width: millimetres(bed.size.x),
                  depth: millimetres(bed.size.y),
                })
              : '',
            t('projectView.bed.copies', { count: bed.instances.length }),
          ]
            .filter((part) => part !== undefined && part !== '')
            .join(', '),
          locked: bed.locked === true,
          picture: bed.thumbnail,
          sequential: bed.sequential === true,
          gcode,
          note,
          changes: bed.layer_changes ?? [],
          filaments: setup.settings.filaments,
        };
      }),
    ),
  );

  // One bed alone shows only what the plan above it does not: the detail a
  // person checks before printing it.
  const shown = $derived(
    only === undefined
      ? cards
      : cards.filter(
          (card) =>
            card.locked ||
            card.sequential ||
            card.gcode ||
            card.note !== '' ||
            card.changes.length > 0,
        ),
  );
</script>

<ul
  class="@container/beds grid grid-cols-1 gap-3 @xl/beds:grid-cols-2 @4xl/beds:grid-cols-3"
  data-slot="project-beds"
>
  {#each shown as card (card.key)}
    <li
      class="grid min-w-0 grid-cols-1 content-start gap-3 rounded-sm border border-border p-3"
    >
      <div class="flex min-w-0 items-start gap-3">
        {#if card.picture !== undefined && thumbnailUrl !== undefined}
          <img
            src={thumbnailUrl(card.picture)}
            alt={t('projectView.bed.picture', { bed: card.label })}
            loading="lazy"
            class="size-16 shrink-0 rounded-xs border border-border bg-muted object-contain"
          />
        {/if}
        <div class="grid min-w-0 flex-1 grid-cols-1 content-start gap-1">
          <h3 class="flex min-w-0 items-center gap-2 text-sm font-medium">
            <span class="truncate" title={card.label}>{card.label}</span>
            {#if card.locked}
              <Badge variant="outline">
                <LockIcon aria-hidden="true" />
                {t('projectView.bed.locked')}
              </Badge>
            {/if}
          </h3>
          <p class="text-xs text-muted-foreground">{card.facts}</p>
          {#if card.sequential || card.gcode}
            <span class="flex flex-wrap gap-1">
              {#if card.sequential}
                <Badge variant="secondary">
                  <ListOrderedIcon aria-hidden="true" />
                  {t('projectView.bed.sequential')}
                </Badge>
              {/if}
              {#if card.gcode}
                <Badge variant="outline">
                  <FileCodeIcon aria-hidden="true" />
                  {t('projectView.bed.gcode')}
                </Badge>
              {/if}
            </span>
          {/if}
        </div>
      </div>
      {#if card.note !== ''}
        <p class="text-xs text-muted-foreground">{card.note}</p>
      {/if}
      <ProjectLayerChanges changes={card.changes} filaments={card.filaments} />
    </li>
  {/each}
</ul>
