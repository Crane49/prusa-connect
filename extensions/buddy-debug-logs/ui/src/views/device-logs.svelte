<!--
  The Logs tab on a printer's page: this printer's lines, compact, and the
  button that asks it to send them here.
-->
<script lang="ts">
  import { Button } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { heldSession } from '../session';
  import { SetupStore } from '../setup.svelte';
  import LogView from './log-view.svelte';
  import SetupDialog from './setup-dialog.svelte';

  let { deviceId }: { deviceId: string } = $props();

  const { host, api, status } = heldSession();
  const t = host.t;

  const setup = new SetupStore(api, host.client, window.location.hostname);
  let setupOpen = $state(false);

  onMount(() => {
    // The printer filter lists the senders no printer is matched to.
    void status.load();
  });
</script>

<section class="@container/logs grid gap-3" data-part="device-logs">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 class="text-base font-semibold">{t('buddyDebugLogs.device.title')}</h2>
    <Button
      size="sm"
      variant="secondary"
      onclick={() => {
        setup.open();
        setupOpen = true;
      }}
    >
      {t('buddyDebugLogs.action.setup')}
    </Button>
  </div>
  <LogView {deviceId} />
</section>

<SetupDialog
  {setup}
  {deviceId}
  open={setupOpen}
  onClose={() => {
    setupOpen = false;
  }}
/>
