<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtensionPlan from './extension-plan.svelte';
  import {
    adds,
    addsTwo,
    addsUIOnly,
    conflict,
    fileTypeConflict,
    loading,
    notOffered,
  } from './fixtures';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Plan',
    component: ExtensionPlan,
  });
</script>

<!--
  An empty plan draws nothing and has no story. A plan that moved between the
  question and the install is refused by the install, and the Extensions
  screen's Plan Changed story shows it.
-->

<Story name="Loading" asChild>
  <ExtensionPlan plan={loading} />
</Story>

<Story name="Adds A Module" asChild>
  <ExtensionPlan plan={adds} />
</Story>

<!-- Each module the plan adds fits the pool alone; the two together do not. -->
<Story name="Adds A UI-Only Module" asChild>
  <ExtensionPlan
    plan={addsUIOnly}
    pool={{
      committed_bytes: 2800 * 1_048_576,
      pool_bytes: 3072 * 1_048_576,
      source: 'detected',
    }}
  />
</Story>

<Story name="Adds Modules Over The Pool" asChild>
  <ExtensionPlan
    plan={addsTwo}
    pool={{
      committed_bytes: 2800 * 1_048_576,
      pool_bytes: 3072 * 1_048_576,
      source: 'detected',
    }}
  />
</Story>

<Story name="Conflict" asChild>
  <ExtensionPlan plan={conflict} />
</Story>

<!-- Another enabled extension reads and draws the same file type. -->
<Story name="File Type Conflict" asChild>
  <ExtensionPlan plan={fileTypeConflict} />
</Story>

<Story name="Not Offered" asChild>
  <ExtensionPlan plan={notOffered} />
</Story>
