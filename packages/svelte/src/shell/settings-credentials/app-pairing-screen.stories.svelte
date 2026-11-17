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
    app_name: 'Kitchen display',
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
    review: source({ code: '', pairing: undefined, loaded: false }),
    ...ports,
  }}
/>

<Story
  name="Looking It Up"
  args={{
    review: source({ pairing: undefined, loaded: false, loading: true }),
    ...ports,
  }}
/>

<!-- The app asked to operate, so read and operate are offered and operate is chosen. -->
<Story name="An App Asks" args={{ review: source(), ...ports }} />

<!-- An app that asks for everything offers every scope. -->
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
