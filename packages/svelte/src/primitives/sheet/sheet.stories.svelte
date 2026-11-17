<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import SheetClose from './sheet-close.svelte';
  import SheetContent from './sheet-content.svelte';
  import SheetDescription from './sheet-description.svelte';
  import SheetFooter from './sheet-footer.svelte';
  import SheetHeader from './sheet-header.svelte';
  import SheetTitle from './sheet-title.svelte';
  import SheetTrigger from './sheet-trigger.svelte';
  import Sheet from './sheet.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Sheet',
    component: Sheet,
  });
</script>

{#snippet printerSettings()}
  <Sheet>
    <SheetTrigger>
      {#snippet child({ props })}
        <Button {...props}>Printer settings</Button>
      {/snippet}
    </SheetTrigger>
    <SheetContent>
      <SheetHeader>
        <SheetTitle>Printer settings</SheetTitle>
        <SheetDescription>
          Changes apply the next time this printer connects.
        </SheetDescription>
      </SheetHeader>
      <SheetFooter>
        <SheetClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">Cancel</Button>
          {/snippet}
        </SheetClose>
        <Button>Save</Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
{/snippet}

<Story name="Default" asChild children={printerSettings} />

<Story name="Open State" asChild>
  <Sheet open>
    <SheetTrigger>
      {#snippet child({ props })}
        <Button {...props}>Printer settings</Button>
      {/snippet}
    </SheetTrigger>
    <SheetContent>
      <SheetHeader>
        <SheetTitle>Printer settings</SheetTitle>
        <SheetDescription>
          Changes apply the next time this printer connects.
        </SheetDescription>
      </SheetHeader>
      <SheetFooter>
        <SheetClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">Cancel</Button>
          {/snippet}
        </SheetClose>
        <Button>Save</Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
</Story>

<Story name="Left Side" asChild>
  <Sheet open>
    <SheetTrigger>
      {#snippet child({ props })}
        <Button {...props}>Printer settings</Button>
      {/snippet}
    </SheetTrigger>
    <SheetContent side="left">
      <SheetHeader>
        <SheetTitle>Printer settings</SheetTitle>
        <SheetDescription>
          Changes apply the next time this printer connects.
        </SheetDescription>
      </SheetHeader>
      <SheetFooter>
        <SheetClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary">Cancel</Button>
          {/snippet}
        </SheetClose>
        <Button>Save</Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
</Story>

<Story
  name="Opens By Keyboard And Returns Focus On Escape"
  tags={['!dev', '!autodocs']}
  asChild
  children={printerSettings}
/>
