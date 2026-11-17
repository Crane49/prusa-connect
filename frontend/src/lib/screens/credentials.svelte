<script lang="ts">
  import type { ApiCredential, CredentialScope } from '@prusa/connect-ui/data';
  import {
    SettingsCredentialsScreen,
    SettingsScreen,
  } from '@prusa/connect-ui/shell';

  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const credentials = $derived(ctx.credentials);

  $effect(() => {
    void credentials.load();
  });
</script>

<SettingsScreen>
  <SettingsCredentialsScreen
    {credentials}
    pairHref={PATHS.pair}
    onCreate={(name: string, days: number, scope: CredentialScope) => {
      void credentials.mint(name, days, scope);
    }}
    onDismiss={() => {
      credentials.dismiss();
    }}
    onRevoke={(credential: ApiCredential) => {
      void credentials.revoke(credential.id);
    }}
  />
</SettingsScreen>
