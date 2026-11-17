<!--
  What the ticked copies of a 3MF project add up to: how many copies of how
  many objects, which extruders they print with, and which beds they stand on.

  With nothing ticked it says how to tick. A page that does something with
  the ticked copies, such as printing them, passes its buttons as `acts`,
  which reads the selections one bed at a time.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import { formatList, t } from '../i18n';
  import { BulkBar } from '@prusa/connect-ui/primitives';
  import { getProjectBrowser } from './browser.svelte';
  import ExtruderChips from './extruder-chips.svelte';
  import { bedLabel } from './labels';
  import type { Project } from './model';
  import type { ProjectSelectionActs } from './selection';

  interface Props {
    project: Project;
    /** The page's buttons for the ticked copies. */
    acts?: Snippet<[ProjectSelectionActs]> | undefined;
  }

  let { project, acts }: Props = $props();

  const browser = getProjectBrowser();

  const summary = $derived(browser.summary);
  const filaments = $derived(project.setups[0]?.settings.filaments ?? []);
  const label = $derived(
    t('projectView.selection.label', {
      copies: summary.copies,
      objects: summary.objects,
    }),
  );
  const beds = $derived(
    formatList(summary.beds.map((ref) => bedLabel(project, ref))),
  );
</script>

<div data-slot="project-selection-bar" aria-live="polite">
  <BulkBar
    count={summary.copies}
    {label}
    clearLabel={t('projectView.plan.clear')}
    onClear={() => {
      browser.clear();
    }}
  >
    {#snippet actions()}
      {#if summary.extruders.length > 1 || (project.extruders ?? []).length > 1}
        <span class="shrink-0">
          <ExtruderChips
            extruders={summary.extruders}
            {filaments}
            label={t('projectView.selection.extruders')}
          />
        </span>
      {/if}
      <span class="min-w-0 truncate text-xs text-muted-foreground" title={beds}>
        {beds}
      </span>
      {#if acts !== undefined}
        {@render acts({
          selections: browser.selections,
          summary,
          clear: () => {
            browser.clear();
          },
        })}
      {/if}
    {/snippet}
    {#snippet idle()}
      <p class="text-xs text-muted-foreground">
        {t('projectView.selection.idle')}
      </p>
    {/snippet}
  </BulkBar>
</div>
