// The application hands the printer icons a lookup over the device-model
// vocabulary (ADR-20261016-qrp). The icon and the store each have tests of their own,
// and this one holds the wire between them, which lives only here.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import fixtures from '../test/ux/fixtures/api.json';

vi.mock('./lib/screens', async () => {
  const { default: Declared } =
    await import('../test/declared-icon-screen.svelte');

  return { screenFor: () => Declared };
});

interface Fixture {
  status: number;
  type: string;
  body: string;
}

const vocabulary = 'GET /api/v1/vocabularies/device-models';

// The recorded vocabulary with one declared row added.
const declaring = (): Fixture => {
  const recorded = (fixtures as Record<string, Fixture>)[vocabulary];
  if (recorded === undefined) throw new Error('no recorded vocabulary');

  const body = JSON.parse(recorded.body) as {
    items: Record<string, unknown>[];
  };
  body.items.push({
    model: 'Bench_One',
    display_name: 'Bench Works Tool Changer',
    vendor: 'Bench Works',
    kind: 'printer',
    icon: 'printer-generic',
    connector: 'bench',
    tool_changer: true,
  });

  return { ...recorded, body: JSON.stringify(body) };
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the application', () => {
  it('names a declared model on a printer icon', async () => {
    const table: Record<string, Fixture> = {
      ...(fixtures as Record<string, Fixture>),
      [vocabulary]: declaring(),
    };
    vi.stubGlobal(
      'fetch',
      vi.fn((input: Request | string) => {
        const href = typeof input === 'string' ? input : input.url;
        const method = typeof input === 'string' ? 'GET' : input.method;
        const row = table[`${method} ${new URL(href, location.href).pathname}`];

        return Promise.resolve(
          new Response(row?.body ?? '{}', {
            status: row?.status ?? 404,
            headers: { 'content-type': row?.type ?? 'application/json' },
          }),
        );
      }),
    );
    const { default: App } = await import('./app.svelte');

    await render(App);

    await expect
      .element(page.getByAltText('Bench Works Tool Changer'))
      .toBeInTheDocument();
  });
});
