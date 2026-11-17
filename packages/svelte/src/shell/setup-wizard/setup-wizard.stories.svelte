<!--
  The wizard as the first page of a server never set up. The steps after the
  setup code need the configuration that code unlocks, so the client here
  refuses every code and the story ends at the code step. The wizard keeps
  its step in the browser, so a reload opens the step last reached.

  Omitted Back From Summary: it needs the steps after the code step.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { SetupFlow, type SetupWizardClient } from '../../data';
  import SetupWizard from './setup-wizard.svelte';

  const { Story } = defineMeta({
    title: 'Shell/SetupWizard',
    component: SetupWizard,
  });

  const refuse = (): Promise<never> =>
    Promise.reject(new Error('That setup code is not this server’s.'));

  const client = {
    getHealth: refuse,
    getSetup: refuse,
    recordSetup: refuse,
    getSetupConfiguration: refuse,
    checkSetupStorage: refuse,
    checkSetupOidc: refuse,
    createSetupCertificates: refuse,
  } as unknown as SetupWizardClient;

  const ignore = (): void => undefined;
  const finish = (): Promise<void> => Promise.resolve();
</script>

<Story name="Welcome" asChild>
  <SetupWizard
    flow={new SetupFlow(client)}
    host="prusa-connect.lan"
    version="0.0.0-dev"
    docsHref="https://help.prusa3d.com"
    onlanguage={ignore}
    onfinish={finish}
  />
</Story>

<Story name="Last Attempt Failed" asChild>
  <SetupWizard
    flow={new SetupFlow(client)}
    host="prusa-connect.lan"
    version="0.0.0-dev"
    lastFailure="listen tcp :80: permission denied"
    onlanguage={ignore}
    onfinish={finish}
  />
</Story>
