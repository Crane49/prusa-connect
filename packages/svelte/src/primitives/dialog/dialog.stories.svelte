<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import DialogClose from './dialog-close.svelte';
  import DialogContent from './dialog-content.svelte';
  import DialogDescription from './dialog-description.svelte';
  import DialogFooter from './dialog-footer.svelte';
  import DialogHeader from './dialog-header.svelte';
  import DialogTitle from './dialog-title.svelte';
  import DialogTrigger from './dialog-trigger.svelte';
  import Dialog from './dialog.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Dialog',
    component: Dialog,
  });
</script>

{#snippet renamePrinter()}
  <Dialog>
    <DialogTrigger>
      {#snippet child({ props })}
        <Button {...props}>Rename printer</Button>
      {/snippet}
    </DialogTrigger>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Rename printer</DialogTitle>
        <DialogDescription>
          Choose a name that helps you find this printer in a list.
        </DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <DialogClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">Cancel</Button>
          {/snippet}
        </DialogClose>
        <Button>Save</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
{/snippet}

<Story name="Default" asChild children={renamePrinter} />

<Story name="Open State" asChild>
  <Dialog open>
    <DialogTrigger>
      {#snippet child({ props })}
        <Button {...props}>Rename printer</Button>
      {/snippet}
    </DialogTrigger>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Rename printer</DialogTitle>
        <DialogDescription>
          Choose a name that helps you find this printer in a list.
        </DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <DialogClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">Cancel</Button>
          {/snippet}
        </DialogClose>
        <Button>Save</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</Story>

<Story
  name="Opens By Keyboard And Returns Focus On Escape"
  tags={['!dev', '!autodocs']}
  asChild
  children={renamePrinter}
/>
