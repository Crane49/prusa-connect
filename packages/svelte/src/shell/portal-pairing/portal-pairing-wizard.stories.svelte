<!--
  One story per phase the wizard draws.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import PortalPairingWizard from './portal-pairing-wizard.svelte';
  import { pairingSource, phaseSource, portalFault } from './story-source';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Portal Pairing Wizard',
    component: PortalPairingWizard,
  });
</script>

<Story name="Loading" asChild>
  <PortalPairingWizard pairing={phaseSource('loading')} />
</Story>

<Story name="Start" asChild>
  <PortalPairingWizard pairing={phaseSource('intro')} />
</Story>

<Story name="Code" asChild>
  <PortalPairingWizard pairing={phaseSource('code')} />
</Story>

<Story name="Code without the filled in address" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      ...phaseSource('code'),
      status: {
        state: 'pending',
        step: 'awaiting_confirmation',
        user_code: 'WDJB-MJHT',
        verification_uri: 'https://beta.prusa3d.com/device',
      },
    })}
  />
</Story>

<Story name="Waiting for confirmation" asChild>
  <PortalPairingWizard pairing={phaseSource('confirm')} />
</Story>

<Story name="Waiting for the team" asChild>
  <PortalPairingWizard pairing={phaseSource('approval')} />
</Story>

<Story name="Portal slow while pending" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      ...phaseSource('confirm'),
      error: portalFault(
        'core.resource/BETA_PORTAL_UNAVAILABLE',
        'the Beta Portal is unavailable',
      ),
    })}
  />
</Story>

<Story name="Paired" asChild>
  <PortalPairingWizard pairing={phaseSource('paired')} />
</Story>

<Story name="Paired without a name" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      phase: 'paired',
      status: {
        state: 'paired',
        product: { slug: 'prusa-connect', name: 'Prusa Connect' },
        device: { identifier: 'instance-1' },
      },
    })}
  />
</Story>

<Story name="Paired but suspended" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      phase: 'paired',
      status: {
        state: 'paired',
        identity: { subject: 'tester-1', name: 'Ada Lovelace' },
        product: { slug: 'prusa-connect', name: 'Prusa Connect' },
        access_suspended: true,
      },
    })}
  />
</Story>

<Story name="Expired" asChild>
  <PortalPairingWizard pairing={phaseSource('expired')} />
</Story>

<Story name="Denied" asChild>
  <PortalPairingWizard pairing={phaseSource('denied')} />
</Story>

<Story name="Revoked" asChild>
  <PortalPairingWizard pairing={phaseSource('revoked')} />
</Story>

<Story name="Clock is wrong" asChild>
  <PortalPairingWizard pairing={phaseSource('clock')} />
</Story>

<Story name="Portal unreachable" asChild>
  <PortalPairingWizard pairing={phaseSource('unreachable')} />
</Story>

<Story name="Rate limited" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      error: {
        code: 'core.resource/BETA_PORTAL_RATE_LIMITED',
        status: 429,
        params: { retry_after_seconds: 90 },
        message: 'wait 90 seconds before pairing again',
        traceId: undefined,
      },
    })}
  />
</Story>

<Story name="Disconnected without revocation" asChild>
  <PortalPairingWizard
    pairing={pairingSource({
      error: portalFault(
        'core.resource/BETA_PORTAL_REVOCATION_INCOMPLETE',
        'local authorization was cleared',
      ),
    })}
  />
</Story>
