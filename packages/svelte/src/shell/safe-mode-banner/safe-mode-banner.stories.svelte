<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SafeModeBanner from './safe-mode-banner.svelte';

  const { Story } = defineMeta({
    title: 'Shell/Safe Mode Banner',
    component: SafeModeBanner,
  });

  const SINCE = '2026-10-30T09:12:00.000Z';
</script>

<Story name="Default" asChild>
  <SafeModeBanner
    since={SINCE}
    restarting={false}
    timedOut={false}
    allowed
    onRestartNormally={() => undefined}
  />
</Story>

<Story name="Restarting" asChild>
  <SafeModeBanner
    since={SINCE}
    restarting
    timedOut={false}
    allowed
    onRestartNormally={() => undefined}
  />
</Story>

<Story name="Timed Out" asChild>
  <SafeModeBanner
    since={SINCE}
    restarting={false}
    timedOut
    allowed
    onRestartNormally={() => undefined}
  />
</Story>

<Story name="Not Allowed" asChild>
  <SafeModeBanner
    since={SINCE}
    restarting={false}
    timedOut={false}
    allowed={false}
    onRestartNormally={() => undefined}
  />
</Story>

<Story name="Update In Progress" asChild>
  <SafeModeBanner
    since={SINCE}
    restarting={false}
    timedOut={false}
    allowed
    refusal="upgrade-busy"
    onRestartNormally={() => undefined}
  />
</Story>
