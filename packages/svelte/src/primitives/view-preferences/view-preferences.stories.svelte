<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Button } from '../button';
  import { PAGE_SIZES } from '../pager/sizes';
  import {
    DEFAULT_VIEW_FIELDS,
    ViewPreferences,
  } from './view-preferences.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/View Preferences',
  });

  // No storage: a story keeps its choices for the visit and never writes the
  // browser's own preferences.
  const preferences = new ViewPreferences({ storage: null });
</script>

<!--
  A store, not a component: what a listing opens with on this browser. The
  story edits one store and prints what it holds.
-->

<Story name="Rows And Default Views" asChild>
  <div class="flex flex-col gap-4 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <span>Rows per page: {preferences.rowsPerPage}</span>
      {#each PAGE_SIZES as size (size)}
        <Button
          size="sm"
          variant={preferences.rowsPerPage === size ? 'default' : 'secondary'}
          onclick={() => {
            preferences.rowsPerPage = size;
          }}
        >
          {size}
        </Button>
      {/each}
    </div>
    {#each DEFAULT_VIEW_FIELDS as field (field.id)}
      <div class="flex flex-wrap items-center gap-2">
        <span class="w-40">{field.id}</span>
        {#each field.modes as mode (mode)}
          <Button
            size="sm"
            variant={preferences.defaultView(field.id) === mode
              ? 'default'
              : 'secondary'}
            onclick={() => {
              preferences.setDefaultView(field.id, mode);
            }}
          >
            {mode}
          </Button>
        {/each}
      </div>
    {/each}
  </div>
</Story>
