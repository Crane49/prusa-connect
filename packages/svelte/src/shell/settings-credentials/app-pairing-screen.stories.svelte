<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { AppPairing } from '../../data';
  import AppPairingScreen from './app-pairing-screen.svelte';
  import type { AppPairingSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/App Pairing',
    component: AppPairingScreen,
  });

  /** Dates are relative to the run rather than fixed. */
  const inMinutes = (minutes: number): string =>
    new Date(Date.now() + minutes * 60_000).toISOString();

  const pairing: AppPairing = {
    user_code: 'BCDF-GHJK',
    app_name: 'PrusaSlicer',
    scopes: ['read', 'operate'],
    client_address: '192.0.2.41',
    expires_at: inMinutes(8),
  };

  const source = (over: Partial<AppPairingSource> = {}): AppPairingSource => ({
    code: 'BCDF-GHJK',
    pairing,
    loading: false,
    loaded: true,
    error: undefined,
    deciding: false,
    outcome: undefined,
    ...over,
  });

  const ports = {
    linked: true,
    onLookup: () => undefined,
    onApprove: () => undefined,
    onDeny: () => undefined,
    credentialsHref: '/settings/tokens',
  };
</script>

<!-- A person opened the page without a code, as from the settings room. -->
<Story
  name="No Code Yet"
  args={{
    ...ports,
    linked: false,
    review: source({ code: '', pairing: undefined, loaded: false }),
  }}
/>

<Story
  name="Looking It Up"
  args={{
    review: source({ pairing: undefined, loaded: false, loading: true }),
    ...ports,
  }}
/>

<!-- The link carried the code, so the page only asks to approve the app. -->
<Story name="An App Asks" args={{ review: source(), ...ports }} />

<!-- A person typed the code the app showed, so the field stays above. -->
<Story
  name="An App Asks By A Typed Code"
  args={{ ...ports, linked: false, review: source() }}
/>

<!-- An app that asks for everything says so. -->
<Story
  name="An App Asks To Administer"
  args={{
    review: source({
      pairing: {
        ...pairing,
        app_name: 'Fleet manager',
        scopes: ['administer'],
      },
    }),
    ...ports,
  }}
/>

<Story
  name="Deciding"
  args={{ review: source({ deciding: true }), ...ports }}
/>

<Story
  name="Approved"
  args={{ review: source({ outcome: 'approved' }), ...ports }}
/>

<Story
  name="Denied"
  args={{ review: source({ outcome: 'denied' }), ...ports }}
/>

<!-- A code nothing answers: mistyped, expired, or already decided. -->
<Story
  name="No Such Pairing"
  args={{
    review: source({
      code: 'ZZZZ-ZZZZ',
      pairing: undefined,
      error: { code: 'core.auth/APP_PAIRING_MISSING', status: 404 },
    }),
    ...ports,
  }}
/>

<!-- An api credential cannot decide a pairing. -->
<Story
  name="Refused"
  args={{
    review: source({
      pairing: undefined,
      error: { code: 'core.auth/SESSION_REQUIRED', status: 403 },
    }),
    ...ports,
  }}
/>
