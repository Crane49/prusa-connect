<!--
  A value a person came to copy: the value itself, readable and selectable,
  the button that copies it, and the outcome underneath.

  The button and the state machine behind it are `copy-button`'s. This adds
  the name, the read-only input and the sentence a person can see. A bare fact
  in a definition list uses the button on its own, and both answer "did it
  work" with the same words.

  The sentence is this field's own live region, so the button draws none of
  its own. Two regions announce the same copy twice.

  At rest the sentence says the input cannot be typed into. The row is there
  to say that first, and to carry the copy's outcome after.

  Focus selects the whole value, so a person who cannot use the clipboard
  copies it with one keystroke.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import CopyButton from '../copy-button/copy-button.svelte';
  import { copyStatusMessage } from '../copy-button/copy-status';
  import type { CopyStatus } from '../copy-button/types';
  import Input from '../input/input.svelte';
  import Label from '../label/label.svelte';
  import type { CopyFieldProps } from './types';

  let { label, value, class: className, ...rest }: CopyFieldProps = $props();

  const id = $props.id();
  const statusId = `${id}-status`;

  let status = $state<CopyStatus>('idle');

  const message = $derived(
    status === 'idle' ? t('primitives.readOnly') : copyStatusMessage(status),
  );
</script>

<div
  {...rest}
  data-slot="copy-field"
  class={['flex flex-col gap-2', className]}
>
  <Label for={id}>{label}</Label>
  <div class="flex gap-2">
    <Input
      {id}
      readonly
      {value}
      class="font-mono"
      onfocus={(event: FocusEvent & { currentTarget: HTMLInputElement }) => {
        event.currentTarget.select();
      }}
    />
    <CopyButton
      {value}
      onStatusChange={(next: CopyStatus) => {
        status = next;
      }}
      announce={false}
      describedBy={statusId}
    />
  </div>
  <p
    id={statusId}
    role="status"
    class={[
      'text-sm',
      status === 'failed' ? 'text-destructive-text' : 'text-muted-foreground',
    ]}
  >
    {message}
  </p>
</div>
