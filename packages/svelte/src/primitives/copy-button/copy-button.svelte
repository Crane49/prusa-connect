<!--
  Copies a value, and says on the spot whether it worked.

  The answer is in place rather than in a toast. A toast is for something that
  happened somewhere else on the page, and a copy happens where the finger
  already is. The glyph changes for the person who sees it, and an `sr-only`
  live region says the same thing in words for the reader who does not.

  The state machine is here, so a caller that draws the outcome itself, as
  `CopyField` does under its input, reads the same four states and the same
  sentences from `copy-status.ts`.

  The accessible name stays the label through every state. Some assistive
  technology does not re-announce a rename on an element that already has
  focus, so the outcome travels in the status region and never in the button's
  own name.

  The button is disabled while the write is in flight. A second write racing
  the first can resolve out of order and leave the wrong status on screen.
-->
<script lang="ts">
  import CopyIcon from '@lucide/svelte/icons/copy';
  import CopyCheckIcon from '@lucide/svelte/icons/copy-check';
  import CopyXIcon from '@lucide/svelte/icons/copy-x';

  import { t } from '../../i18n';
  import Button from '../button/button.svelte';
  import Spinner from '../spinner/spinner.svelte';
  import { copyText } from './copy-text';
  import { copyStatusMessage } from './copy-status';
  import type { CopyButtonProps, CopyStatus } from './types';

  let {
    value,
    label,
    onStatusChange,
    announce = true,
    describedBy,
    size = 'icon-md',
  }: CopyButtonProps = $props();

  const STATUS_RESET_MS = 3000;

  const STATUS_ICON = {
    idle: CopyIcon,
    copied: CopyCheckIcon,
    failed: CopyXIcon,
  };

  let status = $state<CopyStatus>('idle');

  // A caller may name its own words. The default is read from the catalogue
  // when the component draws, so a change of language rewrites it
  // (frontend-i18n.md rule 1).
  const labelText = $derived(label ?? t('primitives.copy'));
  const message = $derived(copyStatusMessage(status));
  const Icon = $derived(status === 'pending' ? undefined : STATUS_ICON[status]);

  const report = (next: CopyStatus): void => {
    status = next;
    onStatusChange?.(next);
  };

  const press = async (): Promise<void> => {
    report('pending');
    try {
      await copyText(typeof value === 'function' ? value() : value);
      report('copied');
    } catch {
      // A browser that refuses the clipboard says so on screen. A press that
      // looks as if it worked and did not is the worse failure.
      report('failed');
    }
  };

  // The outcome goes back to nothing after a moment. The cleanup is what keeps
  // a press just before an unmount from writing to a gone component
  // (svelte-testing.md).
  $effect(() => {
    if (status !== 'copied' && status !== 'failed') return;
    const timer = setTimeout(() => {
      report('idle');
    }, STATUS_RESET_MS);
    return () => {
      clearTimeout(timer);
    };
  });
</script>

<Button
  variant="secondary"
  {size}
  label={labelText}
  aria-describedby={describedBy}
  disabled={status === 'pending'}
  onclick={() => {
    void press();
  }}
>
  {#if Icon}
    <Icon aria-hidden="true" />
  {:else}
    <Spinner />
  {/if}
</Button>
{#if announce}
  <span role="status" class="sr-only">{message}</span>
{/if}
