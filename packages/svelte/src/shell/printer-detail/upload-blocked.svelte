<!-- Why a file cannot be uploaded to this printer, where the upload would be. -->
<script lang="ts">
  import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

  import type { UploadBlock } from '../../domain';
  import { t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Spinner,
  } from '../../primitives';

  let {
    block,
    checking = false,
    onRecheck,
  }: {
    block: UploadBlock;
    /** The printer is being asked again, so the button waits. */
    checking?: boolean | undefined;
    /** Asks the printer again. Absent, or for a block the printer cannot lift, no button. */
    onRecheck?: (() => void) | undefined;
  } = $props();

  const COPY: Record<UploadBlock, { title: string; note: string }> = {
    noStorage: {
      title: 'printerDetail.uploadBlocked.noStorage.title',
      note: 'printerDetail.uploadBlocked.noStorage.note',
    },
    storagesUnknown: {
      title: 'printerDetail.uploadBlocked.storagesUnknown.title',
      note: 'printerDetail.uploadBlocked.storagesUnknown.note',
    },
    noServerStorage: {
      title: 'printerDetail.uploadBlocked.noServerStorage.title',
      note: 'printerDetail.uploadBlocked.noServerStorage.note',
    },
  };

  // Asking the printer again changes nothing about this server's own storage.
  const rechecks = $derived(
    onRecheck !== undefined && block !== 'noServerStorage',
  );
</script>

<Alert variant="warning" data-part="upload-blocked" data-block={block}>
  <HardDriveIcon aria-hidden="true" />
  <AlertTitle>{t(COPY[block].title)}</AlertTitle>
  <AlertDescription class="flex flex-col gap-3">
    <p>{t(COPY[block].note)}</p>
    {#if rechecks}
      <div>
        <Button
          size="sm"
          variant="secondary"
          disabled={checking}
          aria-busy={checking}
          onclick={() => onRecheck?.()}
        >
          {#if checking}
            <Spinner />
          {:else}
            <RefreshCwIcon class="size-4" aria-hidden="true" />
          {/if}
          {t('printerDetail.uploadBlocked.recheck')}
        </Button>
      </div>
    {/if}
  </AlertDescription>
</Alert>
