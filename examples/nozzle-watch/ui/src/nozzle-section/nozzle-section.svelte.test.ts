import {
  describeRefusal,
  UndeclaredRegistration,
} from '@prusa/connect-ui/slots';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import setup from '../index';
import { fakePrinter, type Stub, stubHost } from '../test/host.svelte';

// The whole module, driven the way the app drives it: the stub host runs the
// bundle's own setup function, the setup registers its section, and each case
// renders the component that was registered. Nothing here imports past the
// entry, so what these cases prove is what a person would see on a card.

const PRINTER = 'p3f2a1c9b';

/** A state tree with the two paths this module reads, in degrees. */
const nozzle = (current: number, target: number) => ({
  '/tools/0/nozzle_temp': current,
  '/tools/0/nozzle_target_temp': target,
});

/** Runs the module's setup against a stub host and answers what it registered. */
const started = (state: Record<string, unknown> = nozzle(214.8, 215)): Stub => {
  const stub = stubHost([fakePrinter(PRINTER, state)]);
  void setup(stub.host);

  return stub;
};

/** Draws the section the module registered, refusing to guess when it did not. */
const drawn = async (stub: Stub, deviceId = PRINTER): Promise<void> => {
  const section = stub.registry.sections('device-section')[0];
  expect(
    section,
    'the module registered no device-section section',
  ).toBeDefined();

  await render(section!.component, { props: { deviceId } });
};

// Each case calls `started()` first, so the module-level holder in host.ts
// carries no host between them.
describe('the nozzle-watch section', () => {
  it('registers the one section its manifest declares, and nothing else', () => {
    const stub = started();

    expect(stub.registry.pages()).toEqual([]);
    expect(stub.registry.sections('settings')).toEqual([]);
    expect(stub.registry.refusals()).toEqual([]);

    const sections = stub.registry.sections('device-section');
    expect(sections).toHaveLength(1);
    expect(sections[0]!.id).toBe('nozzle');
    // The title is the manifest's and never the module's: an operator reads
    // the heading before installing, and the running code cannot change it.
    expect(sections[0]!.title).toBe('Nozzle temperature');
  });

  it('shows what the printer reports and how far it is from its target', async () => {
    const stub = started(nozzle(214.8, 215));
    await drawn(stub);

    await expect.element(page.getByText('214.8°C')).toBeInTheDocument();
    await expect.element(page.getByText('target 215°C')).toBeInTheDocument();
    // Within the domain's two-degree band, so the reading has arrived rather
    // than still climbing.
    await expect.element(page.getByText('At temperature')).toBeInTheDocument();
  });

  it('reads a climbing nozzle and a cooling one from the same two numbers', async () => {
    const climbing = started(nozzle(70, 215));
    await drawn(climbing);
    await expect.element(page.getByText('Heating')).toBeInTheDocument();

    const cooling = started(nozzle(180, 0));
    await drawn(cooling);
    await expect.element(page.getByText('Cooling')).toBeInTheDocument();
    await expect.element(page.getByText('no target')).toBeInTheDocument();
  });

  it('follows the fleet, because the fleet is where the reading arrives', async () => {
    const stub = started(nozzle(70, 215));
    await drawn(stub);

    await expect.element(page.getByText('70.0°C')).toBeInTheDocument();
    await expect.element(page.getByText('Heating')).toBeInTheDocument();

    // What the app does when a frame moves this printer's row: the row is
    // replaced whole and the store reassigns. The section holds no copy of it,
    // so it redraws with no work of its own.
    stub.fleet.devices = [fakePrinter(PRINTER, nozzle(214.6, 215))];

    await expect.element(page.getByText('214.6°C')).toBeInTheDocument();
    await expect.element(page.getByText('At temperature')).toBeInTheDocument();
  });

  it('says nothing it cannot know about a printer that is not in the fleet', async () => {
    const stub = started();
    await drawn(stub, 'a-printer-this-person-may-not-see');

    await expect
      .element(page.getByText(/not in the fleet this page read/))
      .toBeInTheDocument();
  });

  it('shows a printer that has reported no temperature as not reported', async () => {
    const stub = started({});
    await drawn(stub);

    await expect.element(page.getByText('Not reported')).toBeInTheDocument();
    await expect.element(page.getByText('no target')).toBeInTheDocument();
  });

  it('is refused the moment it registers a section its manifest does not carry', () => {
    const stub = started();
    const section = stub.registry.sections('device-section')[0];
    expect(section).toBeDefined();

    // The ceiling, from the module's own side. A bundle that oversteps is
    // thrown at rather than quietly ignored, and the host drops it whole
    // (ADR-20260911-4p6 rule 21).
    expect(() => {
      stub.host.register.deviceSection('other', section!.component);
    }).toThrow(UndeclaredRegistration);

    // Still the one section, and a refusal an operator can read on the
    // settings screen rather than a line in a console (ADR-20260911-4p6 rule 26).
    expect(stub.registry.sections('device-section')).toHaveLength(1);

    const refusals = stub.registry.refusals();
    expect(refusals).toHaveLength(1);
    expect(refusals[0]).toMatchObject({
      reason: 'undeclared',
      slot: 'device-section',
      name: 'other',
    });
    expect(describeRefusal(refusals[0]!)).toContain(
      'registered a device-section its manifest does not declare: other',
    );
  });

  it('asks the core for nothing, because the fleet is already on the host', async () => {
    const stub = started();
    await drawn(stub);

    await expect.element(page.getByText('214.8°C')).toBeInTheDocument();

    expect(stub.requests).toEqual([]);
  });

  it('leaves nothing on the window or the document when the card closes', async () => {
    const stub = started();

    // The two long-lived targets, which is where a leak actually lives: a
    // listener on an element of the section's own goes with the element when
    // the section is removed (svelte-testing.md).
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
      const section = stub.registry.sections('device-section')[0];
      expect(section).toBeDefined();
      const screen = await render(section!.component, {
        props: { deviceId: PRINTER },
      });
      await expect.element(page.getByText('214.8°C')).toBeInTheDocument();
      void screen.unmount();
    } finally {
      for (const undo of patched) undo();
    }

    for (const [type, left] of outstanding) {
      expect(left, `${String(left)} ${type} listeners outlived the card`).toBe(
        0,
      );
    }
  });
});
