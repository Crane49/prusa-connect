<!--
  The section as the host draws it at the foot of a printer's Status tab. It
  reads the host this module was handed: the namespace for the address, and
  the client that mints the credential as the person on the page. The
  credential is not tied to this printer: it operates every printer its
  person may operate, so its name does not name one.
-->
<script lang="ts">
  import { describeFault } from '@prusa/connect-ui/i18n';

  import { namespaceOf, printerAddress } from '../../address';
  import { heldHost } from '../../host';
  import { type KeyState, PrusalinkSection } from '../prusalink-section';

  let {
    deviceId,
    origin = window.location.origin,
  }: {
    deviceId: string;
    /** The server's address as the browser reached it. A test hands in its own. */
    origin?: string;
  } = $props();

  const host = heldHost();
  const t = host.t;

  // The page address the host writes holds the namespace the operator
  // installed this module under.
  const namespace = namespaceOf(host.pageHref('')) ?? '';
  const address = $derived(printerAddress(origin, namespace, deviceId));

  let key = $state<KeyState>({ kind: 'none' });

  const create = async (): Promise<void> => {
    key = { kind: 'creating' };

    try {
      const minted = await host.client.createCredential({
        body: {
          // The date tells apart the keys a person made on different days.
          name: t('prusalink.credentialName', {
            date: new Date().toISOString().slice(0, 10),
          }),
          scopes: ['operate'],
        },
      });
      key = { kind: 'created', token: minted.token };
    } catch (thrown) {
      key = { kind: 'failed', fault: describeFault(thrown) };
    }
  };
</script>

<PrusalinkSection {t} {address} {key} onCreate={() => void create()} />
