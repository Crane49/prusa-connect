<!-- The page an app sends a person to: the pairing its code names, which the
     signed-in person approves or denies (ADR-20261025-tpp). -->
<script lang="ts">
  import type { CredentialScope } from '@prusa/connect-ui/data';
  import { AppPairingScreen } from '@prusa/connect-ui/shell';

  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const review = $derived(ctx.appPairing);

  // The code the app's link carried, as verification_uri_complete writes it.
  const code = $derived(new URLSearchParams(ctx.search).get('code') ?? '');

  $effect(() => {
    if (code !== '') void review.load(code);
  });
</script>

<div class="flex flex-col">
  <AppPairingScreen
    {review}
    linked={code !== ''}
    credentialsHref={PATHS.settingsTokens}
    onLookup={(typed: string) => {
      void review.load(typed);
    }}
    onApprove={(scopes: readonly CredentialScope[]) => {
      void review.approve(scopes);
    }}
    onDeny={() => {
      void review.deny();
    }}
  />
</div>
