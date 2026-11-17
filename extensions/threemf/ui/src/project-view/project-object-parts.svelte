<!--
  The parts one object of a 3MF project is built from, and its layer ranges.

  Each part says what it does, which extruder prints its walls, which
  extruders its painting adds, and what else was painted on it. A part that
  prints nothing itself, such as a negative volume, has no extruder. A layer
  range says the heights it covers, the extruder it switches to, and how many
  settings it changes.
-->
<script lang="ts">
  import { t } from '../i18n';
  import { Badge } from '@prusa/connect-ui/primitives';
  import ExtruderChips from './extruder-chips.svelte';
  import { millimetres } from './labels';
  import { filamentsFor, type Project, type ProjectPart } from './model';

  interface Props {
    project: Project;
    /** The object's index in the project's objects. */
    object: number;
  }

  let { project, object }: Props = $props();

  const PART_KEY: Readonly<Record<ProjectPart['type'], string>> = {
    model: 'projectView.part.model',
    negative_volume: 'projectView.part.negative_volume',
    modifier: 'projectView.part.modifier',
    support_blocker: 'projectView.part.support_blocker',
    support_enforcer: 'projectView.part.support_enforcer',
  };

  const target = $derived(project.objects[object]);
  const filaments = $derived(filamentsFor(project, target));

  const painted = (part: ProjectPart): readonly string[] =>
    [
      part.painting?.multimaterial === true ? 'projectView.paint.colours' : '',
      part.painting?.supports === true ? 'projectView.paint.supports' : '',
      part.painting?.seams === true ? 'projectView.paint.seams' : '',
    ].filter((key) => key !== '');
</script>

{#if target !== undefined}
  <div
    class="grid grid-cols-1 content-start gap-3"
    data-slot="project-object-parts"
  >
    <ul class="grid grid-cols-1 content-start gap-2">
      {#each target.parts as part, at (at)}
        <li class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <Badge variant={part.type === 'model' ? 'outline' : 'secondary'}>
            {t(PART_KEY[part.type])}
          </Badge>
          <span class="min-w-0 truncate text-sm" title={part.name}>
            {part.name ?? ''}
          </span>
          {#if part.extruder !== undefined}
            <ExtruderChips
              extruders={[part.extruder]}
              {filaments}
              label={t('projectView.parts.extruder')}
            />
          {/if}
          {#if (part.painted_extruders ?? []).length > 0}
            <span class="text-xs text-muted-foreground">
              {t('projectView.parts.paintedWith')}
            </span>
            <ExtruderChips
              extruders={part.painted_extruders ?? []}
              {filaments}
              label={t('projectView.parts.paintedWith')}
            />
          {/if}
          {#each painted(part) as key (key)}
            <Badge variant="info">{t(key)}</Badge>
          {/each}
        </li>
      {/each}
    </ul>

    {#if (target.layer_ranges ?? []).length > 0}
      <div class="grid grid-cols-1 content-start gap-1.5">
        <h4 class="text-xs text-muted-foreground">
          {t('projectView.ranges.heading')}
        </h4>
        <ul class="grid grid-cols-1 content-start gap-1.5">
          {#each target.layer_ranges ?? [] as range, at (at)}
            <li class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
              <span class="font-mono tabular-nums">
                {t('projectView.ranges.span', {
                  from: millimetres(range.min_z_mm),
                  to: millimetres(range.max_z_mm),
                })}
              </span>
              {#if range.extruder !== undefined}
                <ExtruderChips
                  extruders={[range.extruder]}
                  {filaments}
                  label={t('projectView.ranges.extruder')}
                />
              {/if}
              <span class="text-xs text-muted-foreground">
                {t('projectView.ranges.settings', {
                  count: Object.keys(range.settings).length,
                })}
              </span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  </div>
{/if}
