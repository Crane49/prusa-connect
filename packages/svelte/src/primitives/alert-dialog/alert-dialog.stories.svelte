<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import Button from '../button/button.svelte';
  import AlertDialogAction from './alert-dialog-action.svelte';
  import AlertDialogCancel from './alert-dialog-cancel.svelte';
  import AlertDialogContent from './alert-dialog-content.svelte';
  import AlertDialogDescription from './alert-dialog-description.svelte';
  import AlertDialogFooter from './alert-dialog-footer.svelte';
  import AlertDialogHeader from './alert-dialog-header.svelte';
  import AlertDialogMedia from './alert-dialog-media.svelte';
  import AlertDialogTitle from './alert-dialog-title.svelte';
  import AlertDialogTrigger from './alert-dialog-trigger.svelte';
  import AlertDialog from './alert-dialog.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/AlertDialog',
    component: AlertDialog,
  });
</script>

<!-- bits-ui puts no click handler on the action, so Remove runs the caller's
     own handler and the caller shuts the panel. These stories show the panel,
     so they pass none and Cancel is what shuts it. -->
{#snippet removePrinter()}
  <AlertDialog>
    <AlertDialogTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="destructive">Remove printer</Button>
      {/snippet}
    </AlertDialogTrigger>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Remove printer</AlertDialogTitle>
        <AlertDialogDescription>
          This removes the printer from your farm. Its job history is kept.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive">Remove</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
{/snippet}

<Story name="Default" asChild children={removePrinter} />

<Story name="Open State" asChild>
  <AlertDialog open>
    <AlertDialogTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="destructive">Remove printer</Button>
      {/snippet}
    </AlertDialogTrigger>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Remove printer</AlertDialogTitle>
        <AlertDialogDescription>
          This removes the printer from your farm. Its job history is kept.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive">Remove</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</Story>

<Story
  name="Opens By Keyboard And Returns Focus On Escape"
  tags={['!dev', '!autodocs']}
  asChild
  children={removePrinter}
/>

<Story
  name="Cancel And Action Are Reachable And Scrim Is Inert"
  tags={['!dev', '!autodocs']}
  asChild
  children={removePrinter}
/>

<!-- Beyond the React set. React ships the media part and no story that draws
     it, so this is the only place the side column and the two-column title can
     be looked at. -->
<Story name="With Media" asChild>
  <AlertDialog open>
    <AlertDialogTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="destructive">Remove printer</Button>
      {/snippet}
    </AlertDialogTrigger>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogMedia><TriangleAlertIcon /></AlertDialogMedia>
        <AlertDialogTitle>Remove printer</AlertDialogTitle>
        <AlertDialogDescription>
          This removes the printer from your farm. Its job history is kept.
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction variant="destructive">Remove</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</Story>
