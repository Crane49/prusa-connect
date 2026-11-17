<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import ConfirmDialog from './confirm-dialog.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Confirm Dialog',
    component: ConfirmDialog,
  });
</script>

<script lang="ts">
  let confirmed = $state(0);
</script>

{#snippet deleteTrigger({ props }: { props: Record<string, unknown> })}
  <Button {...props} variant="destructive">Delete</Button>
{/snippet}

{#snippet installTrigger({ props }: { props: Record<string, unknown> })}
  <Button {...props}>Install</Button>
{/snippet}

<!-- The shape every destructive act in the product is confirmed in. -->
<Story name="Before Something Irreversible" asChild>
  <ConfirmDialog
    trigger={deleteTrigger}
    title="Delete &quot;Bracket_Left_Reinforced_v4_FINAL.gcode&quot;?"
    description="This removes the file from the store. This cannot be undone."
    confirmLabel="Delete"
    onConfirm={() => (confirmed += 1)}
  />
</Story>

<!-- A file name can have no space, so it breaks at any letter to stay inside
     the panel. -->
<Story name="A Long File Name" asChild>
  <ConfirmDialog
    open
    trigger={deleteTrigger}
    title="Delete 3dbenchy(3)_0.4n_0.2mm_PETG_COREONEINDX_51m_with_a_much_longer_suffix.bgcode?"
    description="This removes the file from the store. This cannot be undone."
    confirmLabel="Delete"
    onConfirm={() => (confirmed += 1)}
  />
</Story>

<!-- An act that loses nothing, such as an install or an update, confirms with
     the primary button instead of the red one. -->
<Story name="An Act That Loses Nothing" asChild>
  <ConfirmDialog
    trigger={installTrigger}
    title="Install &quot;Spools&quot; 1.2.0?"
    description="It adds a module that keeps a spool list beside your printers."
    confirmLabel="Install"
    variant="confirm"
    onConfirm={() => (confirmed += 1)}
  />
</Story>

<!-- The act held back while what it acts on is still being read. -->
<Story name="Held Back While Reading" asChild>
  <ConfirmDialog
    trigger={installTrigger}
    title="Install &quot;Spools&quot;?"
    description="Reading what the install adds."
    confirmLabel="Install"
    variant="confirm"
    confirmDisabled
    onConfirm={() => (confirmed += 1)}
  />
</Story>

<!-- Both footer buttons are `lg`. This is the one thing the nine hand-built
     copies disagreed about, four at 44px and five at 36. -->
<Story
  name="Both Footer Buttons Are The Same Height"
  tags={['!dev', '!autodocs']}
  asChild
>
  <ConfirmDialog
    trigger={deleteTrigger}
    title="Delete &quot;Bracket_Left_Reinforced_v4_FINAL.gcode&quot;?"
    description="This removes the file from the store. This cannot be undone."
    confirmLabel="Delete"
    onConfirm={() => (confirmed += 1)}
  />
</Story>

<Story name="Confirming Calls Back" tags={['!dev', '!autodocs']} asChild>
  <div>
    <ConfirmDialog
      trigger={deleteTrigger}
      title="Delete &quot;Bracket_Left_Reinforced_v4_FINAL.gcode&quot;?"
      description="This removes the file from the store. This cannot be undone."
      confirmLabel="Delete"
      onConfirm={() => (confirmed += 1)}
    />
    <p>Confirmed {confirmed} times</p>
  </div>
</Story>

<!-- Cancel is the escape, and it must not act. -->
<Story name="Cancelling Does Nothing" tags={['!dev', '!autodocs']} asChild>
  <div>
    <ConfirmDialog
      trigger={deleteTrigger}
      title="Delete &quot;Bracket_Left_Reinforced_v4_FINAL.gcode&quot;?"
      description="This removes the file from the store. This cannot be undone."
      confirmLabel="Delete"
      onConfirm={() => (confirmed += 1)}
    />
    <p>Confirmed {confirmed} times</p>
  </div>
</Story>
