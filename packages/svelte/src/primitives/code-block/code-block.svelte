<!--
  A block of text a person came to read and to copy: a printer object, a
  configuration file, the lines a support ticket needs.

  The height is a definite `h-*` class and never a cap. The scroll area's
  viewport is the full height of its root, and a percentage resolves against a
  definite height and not against a capped one, so `max-h-96` draws the whole
  text and the page scrolls instead of the block. Text too wide for the block
  scrolls inside it, which is what WCAG 1.4.10 asks for in place of a page that
  scrolls sideways.

  The copy machine is written here rather than taken from `CopyButton`,
  because the block already answers underneath itself. The sentence there says
  `Read-only.` at rest and carries the outcome after a press, and it is the
  button's accessible description, so the button needs no live region of its
  own and two regions never announce the same copy twice.
-->
<script lang="ts">
  import CopyIcon from '@lucide/svelte/icons/copy';
  import CopyCheckIcon from '@lucide/svelte/icons/copy-check';
  import CopyXIcon from '@lucide/svelte/icons/copy-x';
  import DownloadIcon from '@lucide/svelte/icons/download';

  import { t } from '../../i18n';
  import Button from '../button/button.svelte';
  import { copyText } from '../copy-button/copy-text';
  import ScrollArea from '../scroll-area/scroll-area.svelte';
  import Spinner from '../spinner/spinner.svelte';
  import type { CodeBlockProps, CopyStatus } from './types';

  let {
    label,
    value,
    heightClass,
    download,
    class: className,
    ...rest
  }: CodeBlockProps = $props();

  /** How long the outcome stands before the block goes back to saying it is read-only. */
  const STATUS_RESET_MS = 3000;

  const id = $props.id();
  const statusId = `${id}-status`;

  let status = $state<CopyStatus>('idle');

  // Read at draw time, so a change of language rewrites the sentence that is
  // already on screen.
  const message = $derived.by(() => {
    if (status === 'pending') return t('primitives.copying');
    if (status === 'copied') return t('primitives.copied');
    if (status === 'failed') return t('primitives.codeBlock.copyFailed');
    return t('primitives.readOnly');
  });

  $effect(() => {
    if (status !== 'copied' && status !== 'failed') return;
    const timer = setTimeout(() => {
      status = 'idle';
    }, STATUS_RESET_MS);
    return () => {
      clearTimeout(timer);
    };
  });

  // A data URL rather than a blob URL: the text is a few lines, and a data URL
  // holds no object that has to be revoked when the value changes.
  const href = $derived(
    `data:text/plain;charset=utf-8,${encodeURIComponent(value)}`,
  );

  const copy = async (): Promise<void> => {
    // Guards a second call made in code. `disabled` already stops a real
    // press, and a raced second write could land out of order and overwrite
    // the right outcome with the wrong one.
    if (status === 'pending') return;
    status = 'pending';
    try {
      await copyText(value);
      status = 'copied';
    } catch {
      status = 'failed';
    }
  };
</script>

<div
  data-slot="code-block"
  class={['flex flex-col gap-2', className]}
  {...rest}
>
  <div
    class={[
      'flex items-center gap-2',
      label ? 'justify-between' : 'justify-end',
    ]}
  >
    {#if label}
      <!-- An unbroken file name's smallest width is the whole string, 250px
           measured for `prusa_printer_settings.ini`, which pushes the copy
           button off the row at 320px. `min-w-0` lets the name shrink and
           `truncate` clips it. -->
      <span class="min-w-0 truncate text-sm font-semibold text-muted-foreground"
        >{label}</span
      >
    {/if}
    <div class="flex items-center gap-2">
      {#if download}
        <Button
          variant="secondary"
          size="icon-md"
          label={t('primitives.download')}
        >
          {#snippet child({ props })}
            <a {...props} type={undefined} {href} {download}>
              <DownloadIcon aria-hidden="true" />
            </a>
          {/snippet}
        </Button>
      {/if}
      <Button
        type="button"
        variant="secondary"
        size="icon-md"
        label={t('primitives.copy')}
        aria-describedby={statusId}
        disabled={status === 'pending'}
        onclick={copy}
      >
        {#if status === 'pending'}
          <Spinner />
        {:else if status === 'copied'}
          <CopyCheckIcon aria-hidden="true" />
        {:else if status === 'failed'}
          <CopyXIcon aria-hidden="true" />
        {:else}
          <CopyIcon aria-hidden="true" />
        {/if}
      </Button>
    </div>
  </div>
  <ScrollArea class={['rounded-sm bg-surface', heightClass ?? 'h-96']}>
    <pre
      class="p-4 font-mono text-xs whitespace-pre text-foreground">{#if value === ''}<span
          class="text-muted-foreground">{t('primitives.codeBlock.empty')}</span
        >{:else}{value}{/if}</pre>
  </ScrollArea>
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
