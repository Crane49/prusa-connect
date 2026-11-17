<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Trash2Icon from '@lucide/svelte/icons/trash-2';

  import Button from '../button/button.svelte';
  import Input from '../input/input.svelte';
  import BulkBar from './bulk-bar.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Bulk Bar',
    component: BulkBar,
  });
</script>

<!-- Nothing ticked: the row belongs to the whole listing. -->
<Story name="Default" asChild>
  <div class="w-full max-w-lg">
    <BulkBar
      count={0}
      label=""
      clearLabel="Clear selection"
      onClear={() => undefined}
    >
      {#snippet idle()}
        <Input class="flex-1" placeholder="Search this folder by name" />
      {/snippet}
    </BulkBar>
  </div>
</Story>

<!-- Ticked: the same row, the same height, a different subject. -->
<Story name="Selected" asChild>
  <div class="w-full max-w-lg">
    <BulkBar
      count={3}
      label="3 selected"
      clearLabel="Clear selection"
      onClear={() => undefined}
    >
      {#snippet actions()}
        <Button variant="destructive" size="sm" class="shrink-0">
          <Trash2Icon aria-hidden="true" />
          Delete
        </Button>
      {/snippet}
    </BulkBar>
  </div>
</Story>

<!-- Some rows ticked: the bar offers the rest of the folder, on every page. -->
<Story name="Offers Every Row" asChild>
  <div class="w-full max-w-lg">
    <BulkBar
      count={3}
      label="3 selected"
      selectAllLabel="Select all 40"
      onSelectAll={() => undefined}
      clearLabel="Clear selection"
      onClear={() => undefined}
    >
      {#snippet actions()}
        <Button variant="destructive" size="sm" class="shrink-0">
          <Trash2Icon aria-hidden="true" />
          Delete
        </Button>
      {/snippet}
    </BulkBar>
  </div>
</Story>
