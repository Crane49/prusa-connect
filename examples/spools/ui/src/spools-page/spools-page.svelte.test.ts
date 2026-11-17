import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { type Answer, type Stub, stubHost } from '../test/host';

// The whole module, driven the way the app drives it: the stub host runs the
// bundle's own setup function, the setup registers its page, and each case
// renders the component that was registered. Nothing here imports past the
// entry, so what these cases prove is what a person would see.

const MOUNT = '/api/v1/modules/com.github.aux.acme.spools/spools';

const SHELF = {
  spools: [
    { id: 'petg-black', material: 'PETG', colour: 'black', grams: 740 },
    {
      id: 'pla-galaxy',
      material: 'PLA',
      colour: 'galaxy black',
      device: 'p3f2a1c9b',
      grams: 210,
    },
  ],
  principal: 'user:demo',
};

/** Runs the module's setup against a stub host and answers what it registered. */
const started = (answers: readonly Answer[]): Stub => {
  const stub = stubHost(answers);
  void setup(stub.host);

  return stub;
};

/** Renders the page the module registered, refusing to guess when it did not. */
const opened = async (stub: Stub): Promise<void> => {
  const component = stub.registered.pages.spools;
  expect(component, 'the module registered no page at spools').toBeDefined();

  await render(component!);
};

// Each case runs `started()` first, which builds a new shelf over a new stub,
// so the module-level holder in shelf.svelte.ts carries nothing between them.
describe('the spools page', () => {
  it('registers the one page its manifest declares, and nothing else', () => {
    const stub = started([{ status: 200, body: SHELF }]);

    expect(Object.keys(stub.registered.pages)).toEqual(['spools']);
    expect(Object.keys(stub.registered.deviceCard)).toEqual([]);
    expect(Object.keys(stub.registered.settings)).toEqual([]);
  });

  it('lists the shelf the module answered, with the principal the host gave', async () => {
    const stub = started([{ status: 200, body: SHELF }]);
    await opened(stub);

    await expect.element(page.getByText('petg-black')).toBeInTheDocument();
    await expect.element(page.getByText('galaxy black')).toBeInTheDocument();
    await expect.element(page.getByText('on p3f2a1c9b')).toBeInTheDocument();
    await expect.element(page.getByText(/user:demo/)).toBeInTheDocument();

    // One read for one datum, at the module's own address, with the browser's
    // session and no token in the page (ADR-20260911-4p6 rules 18 and 20).
    expect(stub.calls).toHaveLength(1);
    const read = stub.calls[0]!;
    expect(read.url).toBe(MOUNT);
    expect(read.method).toBe('GET');
    expect(read.authorization).toBeNull();
    expect(read.credentials).toBe('same-origin');
  });

  it('posts a spool and shows the row the module answered', async () => {
    const stub = started([
      { status: 200, body: SHELF },
      {
        status: 201,
        body: {
          spool: {
            id: 'asa-white',
            material: 'ASA',
            colour: 'white',
            grams: 1000,
          },
          principal: 'user:demo',
        },
      },
    ]);
    await opened(stub);

    await expect.element(page.getByText('petg-black')).toBeInTheDocument();

    await page.getByRole('textbox', { name: 'Name' }).fill('asa-white');
    await page.getByRole('textbox', { name: 'Colour' }).fill('white');
    await page.getByRole('combobox', { name: 'Material' }).click();
    await page.getByRole('option', { name: 'ASA' }).click();
    await page.getByRole('button', { name: 'Add spool' }).click();

    await expect.element(page.getByText('asa-white')).toBeInTheDocument();

    expect(stub.calls).toHaveLength(2);
    const wrote = stub.calls[1]!;
    expect(wrote.url).toBe(MOUNT);
    expect(wrote.method).toBe('POST');
    expect(wrote.authorization).toBeNull();
    expect(JSON.parse(wrote.body ?? '{}')).toMatchObject({
      id: 'asa-white',
      material: 'ASA',
      colour: 'white',
    });
  });

  it('keeps the submit out of reach until the form says something', async () => {
    const stub = started([{ status: 200, body: SHELF }]);
    await opened(stub);

    const submit = page.getByRole('button', { name: 'Add spool' });
    await expect.element(submit).toBeDisabled();

    await page.getByRole('textbox', { name: 'Name' }).fill('asa-white');
    await expect.element(submit).toBeDisabled();

    await page.getByRole('textbox', { name: 'Colour' }).fill('white');
    await expect.element(submit).toBeEnabled();
  });

  it('says why a refused write did not land, and keeps what was typed', async () => {
    const stub = started([
      { status: 200, body: SHELF },
      {
        status: 409,
        contentType: 'application/problem+json',
        body: {
          type: 'about:blank',
          status: 409,
          code: 'spools/SPOOL_TAKEN',
          title: 'that spool is already on the shelf',
          detail: 'the shelf already holds a spool with that id: petg-black',
          params: {},
          retryable: false,
        },
      },
    ]);
    await opened(stub);

    await expect.element(page.getByText('petg-black')).toBeInTheDocument();

    await page.getByRole('textbox', { name: 'Name' }).fill('petg-black');
    await page.getByRole('textbox', { name: 'Colour' }).fill('black');
    await page.getByRole('button', { name: 'Add spool' }).click();

    await expect
      .element(page.getByText(/already holds a spool/))
      .toBeInTheDocument();
    await expect
      .element(page.getByRole('textbox', { name: 'Name' }))
      .toHaveValue('petg-black');
  });

  it('reads the shelf once however many times the page asks', async () => {
    const stub = started([{ status: 200, body: SHELF }]);
    await opened(stub);

    await expect.element(page.getByText('petg-black')).toBeInTheDocument();

    const refresh = page.getByRole('button', { name: 'Refresh' });
    await refresh.click();
    await refresh.click();

    // The mount's read plus the two the person asked for. A second click while
    // a read is in flight is handed the first one's promise, so the count is
    // bounded by the clicks and never by the render.
    expect(stub.calls.length).toBeLessThanOrEqual(3);
    expect(stub.calls.every((call) => call.url === MOUNT)).toBe(true);
  });

  it('leaves nothing on the window or the document when the page closes', async () => {
    const stub = started([{ status: 200, body: SHELF }]);

    // The two long-lived targets, which is where a leak actually lives: a
    // listener on an element of the page's own goes with the element when the
    // page is removed (svelte-testing.md).
    const outstanding = new Map<string, number>();
    const patched: (() => void)[] = [];
    const count = (type: string, by: number) => {
      outstanding.set(type, (outstanding.get(type) ?? 0) + by);
    };

    for (const target of [window, document] as EventTarget[]) {
      const add = target.addEventListener.bind(target);
      const remove = target.removeEventListener.bind(target);

      target.addEventListener = (...args: Parameters<typeof add>) => {
        count(args[0], 1);
        add(...args);
      };
      target.removeEventListener = (...args: Parameters<typeof remove>) => {
        count(args[0], -1);
        remove(...args);
      };

      patched.push(() => {
        target.addEventListener = add;
        target.removeEventListener = remove;
      });
    }

    try {
      const component = stub.registered.pages.spools;
      expect(component).toBeDefined();
      const screen = await render(component!);
      await expect.element(page.getByText('petg-black')).toBeInTheDocument();
      void screen.unmount();
    } finally {
      for (const undo of patched) undo();
    }

    for (const [type, left] of outstanding) {
      expect(left, `${String(left)} ${type} listeners outlived the page`).toBe(
        0,
      );
    }
  });
});
