<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ServerRestartPanel from './server-restart-panel.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Server/Restart',
    component: ServerRestartPanel,
  });

  const FAULT = {
    code: 'core.transport/NETWORK',
    status: undefined,
    params: {},
    message: 'the server could not be reached',
    traceId: undefined,
  };
</script>

<Story name="Normal" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="idle"
    onRestart={() => undefined}
  />
</Story>

<Story name="Safe Mode" asChild>
  <ServerRestartPanel
    safeMode
    allowed
    phase="idle"
    onRestart={() => undefined}
  />
</Story>

<Story name="Confirming" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="idle"
    confirming
    onRestart={() => undefined}
  />
</Story>

<Story name="Restarting" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="waiting"
    onRestart={() => undefined}
  />
</Story>

<Story name="Sending" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="sending"
    onRestart={() => undefined}
  />
</Story>

<Story name="Timed Out" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="timeout"
    onRestart={() => undefined}
  />
</Story>

<Story name="Not Allowed" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed={false}
    phase="idle"
    onRestart={() => undefined}
  />
</Story>

<Story name="Update In Progress" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="idle"
    refusal="upgrade-busy"
    onRestart={() => undefined}
  />
</Story>

<Story name="Failed" asChild>
  <ServerRestartPanel
    safeMode={false}
    allowed
    phase="failed"
    error={FAULT}
    onRestart={() => undefined}
  />
</Story>

<Story name="Safe Mode Restarting" asChild>
  <ServerRestartPanel
    safeMode
    allowed
    phase="waiting"
    onRestart={() => undefined}
  />
</Story>

<Story name="Safe Mode Timed Out" asChild>
  <ServerRestartPanel
    safeMode
    allowed
    phase="timeout"
    onRestart={() => undefined}
  />
</Story>

<Story name="Safe Mode Not Allowed" asChild>
  <ServerRestartPanel
    safeMode
    allowed={false}
    phase="idle"
    onRestart={() => undefined}
  />
</Story>
