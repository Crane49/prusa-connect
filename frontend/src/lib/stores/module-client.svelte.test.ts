// The client a module is handed ends with the person who admitted it. The
// browser's cookie belongs to whoever signs in next, so a request a module
// makes after that would act with the next person's authority.
import { describe, expect, it, vi } from 'vitest';

import { PrusaConnect } from '@prusa/connect';
import { Fleet } from '@prusa/connect-ui/data';
import {
  createModuleLoader,
  defineModule,
  type ModuleHost,
  SlotRegistry,
} from '@prusa/connect-ui/slots';

import PlainPage from '../../test/plain-page.svelte';
import { json } from '../../test/stub';

const NAMESPACE = 'com.github.aux.acme.spools';

const listing = {
  items: [
    {
      config: {},
      enabled: true,
      generation: 1,
      kind: 'module',
      name: NAMESPACE,
      namespace: NAMESPACE,
      required: false,
      runtime: 'asset',
      source: 'file',
      status: {
        attempt: 0,
        conditions: [],
        dropped: 0,
        observed_generation: 1,
        serving: true,
        state: 'ready',
      },
      type: NAMESPACE,
      ui: {
        entry_url: `/api/v1/modules/${NAMESPACE}/ui/index.js`,
        library: '>=0.0.0',
        contributes: {
          pages: [{ path: 'spools', title: 'Spools', hidden: false }],
        },
      },
      uid: `uid-${NAMESPACE}`,
      version: '1',
    },
  ],
};

// A fetch that answers the loader's own reads and leaves the printer listing
// unanswered until its signal aborts, the way a real fetch does.
const serverWith = () => {
  const asked: string[] = [];
  const fetcher: typeof fetch = (input, init) => {
    // The transport always passes the address as a string.
    const address = typeof input === 'string' ? input : '';
    const path = new URL(address, 'http://core.test').pathname;
    asked.push(path);
    if (path === '/api/v1/resources') {
      return Promise.resolve(json(200, listing));
    }
    if (path === '/api/v1/principal') {
      return Promise.resolve(json(200, { principal: 'user:ada' }));
    }
    if (path.startsWith('/api/v1/grants/')) {
      return Promise.resolve(json(200, { devices: [], subject: 'user:ada' }));
    }

    return new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        reject(new DOMException('aborted', 'AbortError'));
      });
    });
  };

  return {
    asked,
    client: new PrusaConnect({ auth: { kind: 'session' }, fetch: fetcher }),
  };
};

describe('the client a module is handed', () => {
  it('aborts a request in flight at the reset and sends none after it', async () => {
    const { asked, client } = serverWith();
    let held: ModuleHost | undefined;
    const loader = createModuleLoader({
      client,
      registry: new SlotRegistry(),
      fleet: new Fleet(client),
      storage: null,
      readCatalogue: () => Promise.resolve(undefined),
      importModule: () =>
        Promise.resolve({
          default: defineModule((host) => {
            held = host;
            host.register.page('spools', PlainPage);
          }),
        }),
    });
    await loader.activate();
    const host = held;
    if (host === undefined) throw new Error('the module was not admitted');

    const inFlight = host.client.listDevices();
    await vi.waitFor(() => {
      expect(asked).toContain('/api/v1/devices');
    });

    loader.reset();

    await expect(inFlight).rejects.toMatchObject({ name: 'AbortError' });
    const before = asked.length;
    await expect(host.client.listDevices()).rejects.toMatchObject({
      name: 'AbortError',
    });
    expect(asked).toHaveLength(before);
  });
});
