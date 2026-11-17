<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SettingsDiagnosticsScreen from './settings-diagnostics-screen.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Diagnostics',
    component: SettingsDiagnosticsScreen,
  });

  const onDownload = () => Promise.resolve();
  const onSend = () => Promise.resolve();
  const onCopy = () => 'Browser: Storybook\nLocale: en';
</script>

<!--
  A failed download or send is drawn as a fault after a click; the screen holds
  that fault itself, so it has no story of its own here.
-->

<Story name="Download Only" asChild>
  <SettingsDiagnosticsScreen {onDownload} {onCopy} />
</Story>

<Story name="Sends To Support" asChild>
  <SettingsDiagnosticsScreen {onDownload} {onCopy} {onSend} />
</Story>

<Story name="With Extensions" asChild>
  <SettingsDiagnosticsScreen
    {onDownload}
    {onCopy}
    {onSend}
    extensions={['Fleet statistics', 'Printer log lines']}
  />
</Story>
