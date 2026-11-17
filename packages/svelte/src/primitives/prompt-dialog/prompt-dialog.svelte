<!--
  A dialog whose whole body is one text field: a title, the field, Cancel and
  a confirm button. New folder and Rename both need exactly this shape and
  nothing more.

  It holds no draft of its own. Clearing one on Cancel, on Escape or on a
  click outside is the caller's `onOpenChange` to get right, and the caller
  resets `value` there whenever `open` goes false. A name typed and then
  abandoned must never come back filled in the next time the dialog opens.
-->
<script lang="ts">
  import type { Attachment } from 'svelte/attachments';
  import { createAttachmentKey } from 'svelte/attachments';

  import { t } from '../../i18n';
  import Button from '../button/button.svelte';
  import Dialog from '../dialog/dialog.svelte';
  import DialogClose from '../dialog/dialog-close.svelte';
  import DialogContent from '../dialog/dialog-content.svelte';
  import DialogDescription from '../dialog/dialog-description.svelte';
  import DialogFooter from '../dialog/dialog-footer.svelte';
  import DialogHeader from '../dialog/dialog-header.svelte';
  import DialogTitle from '../dialog/dialog-title.svelte';
  import Field from '../field/field.svelte';
  import Input from '../input/input.svelte';
  import type { PromptDialogProps } from './types';

  let {
    open = $bindable(false),
    onOpenChange,
    title,
    description,
    label,
    placeholder,
    type = 'text',
    autocomplete,
    value,
    onValueChange,
    error,
    allowEmpty = false,
    pending = false,
    confirmLabel,
    onConfirm,
  }: PromptDialogProps = $props();

  const cancelText = $derived(t('primitives.cancel'));

  const blocked = $derived(
    (!allowEmpty && value.trim() === '') || error !== undefined || pending,
  );

  // `Field` takes a string, and an empty one is no error at all.
  const fieldError = $derived(error ?? '');

  const confirm = (): void => {
    if (blocked) return;
    onConfirm();
  };

  // Focus follows the dialog opening, not the page load. The panel mounts a
  // fresh input every time, so this runs on every open.
  const focusOnOpen = createAttachmentKey();
  const focus: Attachment = (node) => {
    if (node instanceof HTMLElement) node.focus();
  };
</script>

<!-- While the confirm waits on its answer, nothing closes the panel. -->
<Dialog
  bind:open
  onOpenChange={(next: boolean) => {
    onOpenChange(next);
  }}
>
  <DialogContent
    escapeKeydownBehavior={pending ? 'ignore' : 'close'}
    interactOutsideBehavior={pending ? 'ignore' : 'close'}
    showCloseButton={!pending}
  >
    <DialogHeader>
      <DialogTitle>{title}</DialogTitle>
      {#if description}
        <DialogDescription>{description}</DialogDescription>
      {/if}
    </DialogHeader>
    <Field
      {label}
      error={fieldError}
      control={Input}
      controlProps={{
        value,
        placeholder,
        type,
        autocomplete,
        oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
          onValueChange(event.currentTarget.value);
        },
        onkeydown: (event: KeyboardEvent) => {
          if (event.key === 'Enter') confirm();
        },
        [focusOnOpen]: focus,
      }}
    />
    <DialogFooter>
      <DialogClose>
        {#snippet child({ props })}
          <Button {...props} variant="secondary" disabled={pending}>
            {cancelText}
          </Button>
        {/snippet}
      </DialogClose>
      <Button disabled={blocked} aria-busy={pending} onclick={confirm}>
        {confirmLabel}
      </Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
