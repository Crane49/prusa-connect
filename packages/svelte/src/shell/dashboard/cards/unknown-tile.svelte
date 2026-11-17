<!--
  A card of a type no installed module declares, such as one whose module was
  removed, or one a later version of a module added. It says so and keeps the
  card, so saving the dashboard loses nothing (ADR-20261008-sww rule 23). The type
  stays under the note, as the one detail that names the missing module.
-->
<script lang="ts">
  import PuzzleIcon from '@lucide/svelte/icons/puzzle';

  import { t } from '../../../i18n';
  import { EmptyState } from '../../../primitives';

  let { type }: { type: string } = $props();
</script>

<EmptyState
  size="sm"
  icon={PuzzleIcon}
  heading={t('dashboard.unknown.heading')}
  description={t('dashboard.unknown.note')}
>
  {#snippet action()}
    <code class="font-mono text-xs text-muted-foreground">{type}</code>
  {/snippet}
</EmptyState>
