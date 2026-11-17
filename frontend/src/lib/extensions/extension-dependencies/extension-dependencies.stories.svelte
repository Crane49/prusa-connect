<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtensionDependencies from './extension-dependencies.svelte';
  import {
    allMet,
    blocked,
    READER,
    requiredBy,
    unmet,
    updatePossible,
    whyReader,
  } from './fixtures';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Dependencies',
    component: ExtensionDependencies,
  });
</script>

<!--
  The refusals MODULE_REQUIRED_BY and MODULE_DEPENDENCY_MISSING are drawn by
  the Extensions screen, and the locked switch of a required extension by its
  Required By story.
-->

<Story name="All Met" asChild>
  <ExtensionDependencies dependencies={allMet} />
</Story>

<Story name="Unmet Suggested And In Conflict" asChild>
  <ExtensionDependencies dependencies={unmet} />
</Story>

<Story name="Required By" asChild>
  <ExtensionDependencies dependencies={requiredBy} />
</Story>

<Story name="Why And An Update" asChild>
  <ExtensionDependencies
    dependencies={requiredBy}
    namespace={READER}
    why={whyReader}
    outdated={updatePossible}
  />
</Story>

<Story name="A Blocked Update" asChild>
  <ExtensionDependencies
    dependencies={requiredBy}
    namespace={READER}
    why={whyReader}
    outdated={blocked}
  />
</Story>

<Story name="Reading" asChild>
  <ExtensionDependencies
    dependencies={requiredBy}
    namespace={READER}
    why={{ state: 'loading' }}
    outdated={{ state: 'loading' }}
  />
</Story>

<Story name="Not Answered" asChild>
  <ExtensionDependencies
    dependencies={requiredBy}
    namespace={READER}
    why={{ state: 'refused', error: new Error('offline') }}
    outdated={{ state: 'refused', error: new Error('offline') }}
  />
</Story>
