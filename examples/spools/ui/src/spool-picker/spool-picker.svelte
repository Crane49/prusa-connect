<!-- The shelf as a list to pick from. This module hands it to other modules'
     bundles under ui.exports (ADR-20261027-wvj rule 19), so a planner or a
     printer's tab can let a person pick a spool without knowing how this
     module keeps them. The pick goes back through `onselect`. -->
<script lang="ts">
  import {
    Alert,
    AlertDescription,
    Button,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { heldHost } from '../host';
  import { heldShelf } from '../shelf.svelte';
  import type { SpoolPickerOptions } from './types';

  let { onselect, selected }: SpoolPickerOptions = $props();

  const shelf = heldShelf();
  // The texts are this module's own, whichever page draws the picker.
  const t = heldHost().t;

  onMount(() => {
    void shelf.read();
  });
</script>

{#if !shelf.loaded && shelf.problem === ''}
  <p class="text-sm text-muted-foreground">{t('spools.reading')}</p>
{:else if shelf.problem !== ''}
  <Alert variant="destructive">
    <AlertDescription>{shelf.problem}</AlertDescription>
  </Alert>
{:else if shelf.rows.length === 0}
  <p class="text-sm text-muted-foreground">{t('spools.empty')}</p>
{:else}
  <ul class="flex flex-wrap gap-2" aria-label={t('spools.picker.label')}>
    {#each shelf.rows as spool (spool.id)}
      <li>
        <Button
          size="sm"
          variant={spool.id === selected ? 'default' : 'secondary'}
          aria-pressed={spool.id === selected}
          onclick={() => onselect(spool)}
        >
          {spool.id}
        </Button>
      </li>
    {/each}
  </ul>
{/if}
