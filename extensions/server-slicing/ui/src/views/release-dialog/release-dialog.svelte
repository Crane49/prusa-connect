<!--
  The one question the Slicers tab asks before it acts: an install of a
  release with no published checksum, or a removal.
-->
<script lang="ts">
  import { ConfirmDialog } from '@prusa/connect-ui/primitives';

  import type { Translate } from '../../api';
  import type { ReleaseActs } from '../../release-acts.svelte';

  let { acts, t }: { acts: ReleaseActs; t: Translate } = $props();

  const asking = $derived(acts.asking);
</script>

{#if asking?.kind === 'unverified'}
  <ConfirmDialog
    open
    onOpenChange={(open) => {
      if (!open) acts.dismiss();
    }}
    title={t('slicer.unverified.title', { version: asking.version })}
    description={t('slicer.unverified.description')}
    confirmLabel={t('slicer.unverified.confirm')}
    onConfirm={() => acts.confirm()}
  />
{:else if asking?.kind === 'remove'}
  <ConfirmDialog
    open
    onOpenChange={(open) => {
      if (!open) acts.dismiss();
    }}
    title={t('slicer.remove.title', { version: asking.version })}
    description={t('slicer.remove.description')}
    confirmLabel={t('slicer.action.remove')}
    onConfirm={() => acts.confirm()}
  />
{/if}
