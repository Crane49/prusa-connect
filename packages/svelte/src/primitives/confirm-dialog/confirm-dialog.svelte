<!--
  The question asked before something irreversible happens, in one shape.

  Nine screens built it out of the alert dialog's parts, and the parts carry a
  size: four passed `size="lg"` to both footer buttons and five left them at
  the default, so the same Delete was 44px tall on one screen and 36px on
  another. The rest of the shape was already identical everywhere, which is
  what made the size the only thing anyone had to decide and the only thing
  that drifted.

  It is destructive unless the caller says otherwise. Most call sites confirm
  a delete, a revoke, a stop or a firmware flash. An install or an update asks
  before it acts too, but loses nothing, so it takes the `confirm` variant and
  its button is the primary one rather than the red one.

  Two ways to open it, because the screens genuinely differ: a `trigger` for
  one whose button sits right there, or `open` and `onOpenChange` for one a
  screen holds open because the row that asked was a menu item that has
  already shut its own menu.

  One its trigger opened shuts when the act is confirmed, because nothing
  outside holds it. One a screen holds open stays up until the screen shuts
  it, so a slow act has somewhere to say how it went.

  What a caller draws between the description and the buttons can be longer
  than a screen, such as an install that lists every module it adds. So the
  panel is held to the window, the middle scrolls, and the title and the
  buttons stay in view (`dialog/variants.ts`).
-->
<script lang="ts">
  import { t } from '../../i18n';
  import AlertDialog from '../alert-dialog/alert-dialog.svelte';
  import AlertDialogAction from '../alert-dialog/alert-dialog-action.svelte';
  import AlertDialogCancel from '../alert-dialog/alert-dialog-cancel.svelte';
  import AlertDialogContent from '../alert-dialog/alert-dialog-content.svelte';
  import AlertDialogDescription from '../alert-dialog/alert-dialog-description.svelte';
  import AlertDialogFooter from '../alert-dialog/alert-dialog-footer.svelte';
  import AlertDialogHeader from '../alert-dialog/alert-dialog-header.svelte';
  import AlertDialogTitle from '../alert-dialog/alert-dialog-title.svelte';
  import AlertDialogTrigger from '../alert-dialog/alert-dialog-trigger.svelte';
  import { DIALOG_BODY, DIALOG_SCROLLS } from '../dialog/variants';
  import type { ConfirmDialogProps } from './types';

  let {
    open = $bindable(false),
    onOpenChange,
    trigger,
    title,
    description,
    children,
    confirmLabel,
    onConfirm,
    confirmDisabled = false,
    variant = 'destructive',
  }: ConfirmDialogProps = $props();
</script>

<AlertDialog bind:open onOpenChange={(next: boolean) => onOpenChange?.(next)}>
  {#if trigger}
    <AlertDialogTrigger child={trigger} />
  {/if}
  <AlertDialogContent class={children ? DIALOG_SCROLLS : undefined}>
    <AlertDialogHeader>
      <AlertDialogTitle>
        {#if typeof title === 'string'}{title}{:else}{@render title()}{/if}
      </AlertDialogTitle>
      <AlertDialogDescription>
        {#if typeof description === 'string'}{description}{:else}{@render description()}{/if}
      </AlertDialogDescription>
    </AlertDialogHeader>
    {#if children}
      <div
        data-slot="dialog-body"
        class={['flex flex-col gap-4 empty:hidden', DIALOG_BODY]}
      >
        {@render children()}
      </div>
    {/if}
    <AlertDialogFooter>
      <!-- `lg` on both: this is the one dialog in the product where a mis-tap
           costs a file, and the two buttons have to be the same height or the
           pair reads as one control and one afterthought. -->
      <AlertDialogCancel size="lg">{t('primitives.cancel')}</AlertDialogCancel>
      <AlertDialogAction
        variant={variant === 'confirm' ? 'default' : 'destructive'}
        size="lg"
        disabled={confirmDisabled}
        onclick={() => {
          onConfirm();
          if (trigger) open = false;
        }}
      >
        {#if typeof confirmLabel === 'string'}{confirmLabel}{:else}{@render confirmLabel()}{/if}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
