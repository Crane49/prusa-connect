<!--
  The section as the host draws it under the Connection card of a printer's Printer tab. It
  reads the host this module was handed: the namespace for the address, the
  client that mints the credential as the person on the page, and the
  storages that person may save the printer's uploads in. The credential is
  not tied to this printer: it operates every printer its person may operate,
  so its name does not name one.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { createStorage } from '@prusa/connect-ui/data';
  import { describeFault, type FaultText } from '@prusa/connect-ui/i18n';
  import type { SaveToStorageProps } from '@prusa/connect-ui/shell';

  import { namespaceOf, printerAddress } from '../../address';
  import { agoOf } from '../../ago';
  import { heldHost } from '../../host';
  import { type Keep, type KeepState, keepsOn } from '../../keep';
  import {
    type KeyState,
    PrusalinkSection,
    type SeenState,
  } from '../prusalink-section';

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

  // Save to storage, as the module holds it for this printer, and the
  // storages the person may write to, as a drop on the printer's page offers.
  const route = keepsOn(host.client, namespace);
  const storage = createStorage(host.client);

  let saved = $state<KeepState | undefined>(undefined);
  let keepFault = $state<FaultText | undefined>(undefined);

  // The host hands the props again whenever the printer reports, with the
  // same id. A derived value tells only a new id to the effect, so the
  // switch is read once per printer and does not blink with each report.
  const printerId = $derived(deviceId);

  // It runs again only for another printer. The reads write state the store
  // and this effect also read, which would start them over without end.
  $effect(() => {
    const printer = printerId;
    untrack(() => {
      read(printer);
    });
  });

  const read = (printer: string): void => {
    saved = undefined;
    keepFault = undefined;
    void storage.load();
    route.read(printer).then(
      (read) => {
        if (printer === printerId) saved = read;
      },
      (thrown: unknown) => {
        if (printer === printerId) keepFault = describeFault(thrown);
      },
    );
  };

  // When PrusaSlicer last reached the printer. A person who runs Test in
  // PrusaSlicer with this page open sees it within the interval, and the
  // minutes move on with it.
  const SEEN_EVERY = 30_000;
  let seenAt = $state<string | null | undefined>(undefined);
  let now = $state(Date.now());

  $effect(() => {
    const printer = printerId;

    return untrack(() => {
      seenAt = undefined;
      const look = (): void => {
        now = Date.now();
        route.seen(printer).then(
          (read) => {
            if (printer === printerId) seenAt = read.seen_at;
          },
          () => {
            // A card that cannot read it says nothing about it.
          },
        );
      };
      look();
      const timer = setInterval(look, SEEN_EVERY);

      return () => {
        clearInterval(timer);
      };
    });
  });

  const seen = $derived.by((): SeenState => {
    if (seenAt === undefined) return { kind: 'unknown' };
    if (seenAt === null) return { kind: 'never' };

    return { kind: 'at', ago: agoOf(seenAt, now) };
  });

  // The switch moves at once and goes back when the module refuses.
  const put = async (next: Keep): Promise<void> => {
    const before = saved;
    saved = { ...next, on: next.storage !== '', may_change: true };
    keepFault = undefined;

    try {
      const written = await route.write(deviceId, next);
      saved = { ...written, on: written.storage !== '', may_change: true };
    } catch (thrown) {
      saved = before;
      keepFault = describeFault(thrown);
    }
  };

  // A person who may not change the setting reads it, rather than pressing a
  // switch that only fails.
  const keepReadOnly = $derived.by(() => {
    if (saved === undefined || saved.may_change) return undefined;

    return { on: saved.on, nobody: saved.refused === 'check' };
  });

  // The saved storage is the one shown while it is on, even one this person
  // may not write to, so changing the folder never moves the uploads to
  // another storage unasked.
  const keepElsewhere = $derived(
    saved !== undefined &&
      saved.may_change &&
      saved.storage !== '' &&
      !storage.writable.some((row) => row.name === saved?.storage),
  );

  const keep = $derived.by((): SaveToStorageProps | undefined => {
    const first = storage.writable[0];
    if (!saved?.may_change) return undefined;
    if (first === undefined && saved.storage === '') return undefined;

    const held = saved;
    const shown = held.storage !== '' ? held.storage : (first?.name ?? '');
    const storages = storage.writable.map((row) => ({
      name: row.name,
      label: row.display_name,
    }));
    if (!storages.some((row) => row.name === shown)) {
      storages.push({ name: shown, label: shown });
    }

    return {
      keep: held.storage !== '',
      onKeepChange: (on: boolean) => {
        void put(
          on ? { storage: shown, folder: '' } : { storage: '', folder: '' },
        );
      },
      storages,
      storage: shown,
      onStorageChange: (name: string) => {
        void put({ storage: name, folder: '' });
      },
      folder: held.storage === shown ? held.folder : '',
      onFolderChange: (path: string) => {
        void put({ storage: shown, folder: path });
      },
      pickerAt: storage.pickerPath,
      pickerFolders: storage.pickerFolders,
      pickerFault: storage.pickerError,
      onPickerWalk: (path: string) => {
        void storage.browseFolders(shown, path);
      },
    };
  });
</script>

<PrusalinkSection
  {t}
  {address}
  {key}
  onCreate={() => void create()}
  {keep}
  {keepFault}
  {keepReadOnly}
  {keepElsewhere}
  {seen}
/>
