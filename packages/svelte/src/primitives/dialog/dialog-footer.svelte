<!--
  The band of buttons at the foot of the panel. Below the `sm` width they stack
  and the primary one sits at the bottom, which is the phone shape.
-->
<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';

  import { t } from '../../i18n';
  import Button from '../button/button.svelte';
  import type { DialogFooterProps } from './types';

  let {
    children,
    class: className,
    showCloseButton = false,
    ...rest
  }: DialogFooterProps = $props();

  const closeText = $derived(t('primitives.close'));
</script>

<div
  data-slot="dialog-footer"
  class={['flex flex-col-reverse gap-2 sm:flex-row sm:justify-end', className]}
  {...rest}
>
  {@render children?.()}
  {#if showCloseButton}
    <DialogPrimitive.Close>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">{closeText}</Button>
      {/snippet}
    </DialogPrimitive.Close>
  {/if}
</div>
