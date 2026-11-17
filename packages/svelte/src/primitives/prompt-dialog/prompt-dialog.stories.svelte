<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import PromptDialogNewFolder from '../../test/prompt-dialog-new-folder.svelte';
  import PromptDialogRename from '../../test/prompt-dialog-rename.svelte';

  // No `component`: the dialog holds no draft, so every story drives one of
  // the two demos that own a name, the way the React stories do.
  const { Story } = defineMeta({
    title: 'Primitives/Prompt Dialog',
  });
</script>

<Story name="New Folder" asChild>
  <PromptDialogNewFolder />
</Story>

<Story name="Rename" asChild>
  <PromptDialogRename />
</Story>

<Story name="Empty Name Disables Confirm" tags={['!dev', '!autodocs']} asChild>
  <PromptDialogNewFolder />
</Story>

<Story
  name="Duplicate Name Blocks Confirm"
  tags={['!dev', '!autodocs']}
  asChild
>
  <PromptDialogNewFolder />
</Story>

<Story
  name="Creating Closes The Dialog And Reports Success"
  tags={['!dev', '!autodocs']}
  asChild
>
  <PromptDialogNewFolder />
</Story>

<Story name="Cancel Discards The Draft" tags={['!dev', '!autodocs']} asChild>
  <PromptDialogNewFolder />
</Story>

<Story
  name="Rename Keeps The Current Name After Cancel"
  tags={['!dev', '!autodocs']}
  asChild
>
  <PromptDialogRename currentName="benchy.3mf" />
</Story>
