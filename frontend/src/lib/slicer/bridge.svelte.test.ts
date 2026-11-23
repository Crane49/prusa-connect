// The page inside PrusaSlicer (ADR-20261112-2eb rules 4 and 5). It runs in
// Chromium because the bridge reads clicks on real links.
import { createSlicer, type SlicerMessage } from '@prusa/connect-ui/slicer';
import { afterEach, describe, expect, it } from 'vitest';

import {
  ASK_MS,
  type BridgeWindow,
  REFRESH_MS,
  type SlicerLook,
  startSlicerBridge,
} from './bridge';

interface Rig {
  page: BridgeWindow;
  sent: SlicerMessage[];
  // Whether the cookie the slicer set holds a person, as the core answers.
  cookie: boolean;
  // How often the bridge asked the core again.
  rechecks: number;
  // The timers the bridge set, by delay.
  timers: { ms: number; run: () => void }[];
  ended: number;
  reloads: number;
  looks: SlicerLook[];
  heard: { type: string; detail: unknown }[];
  signedIn: boolean;
}

const cleanups: (() => void)[] = [];

afterEach(() => {
  for (const cleanup of cleanups.splice(0)) cleanup();
});

// Lets the bridge's check of the session answer.
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

const rig = (
  options: { inSlicer?: boolean; signedIn?: boolean; cookie?: boolean } = {},
): Rig & { bridge: ReturnType<typeof startSlicerBridge> } => {
  const target = new EventTarget();
  const sent: SlicerMessage[] = [];
  const heard: Rig['heard'] = [];
  const page: BridgeWindow = {
    _prusaSlicer:
      options.inSlicer === false
        ? undefined
        : {
            postMessage: (message) => {
              sent.push(message);
            },
          },
    // A click reaches the window from a link in the document, so the test
    // page hears clicks on the real document.
    addEventListener: (type, listener) => {
      if (type === 'click') {
        document.addEventListener(type, listener);
        cleanups.push(() => {
          document.removeEventListener(type, listener);
        });
      } else {
        target.addEventListener(type, listener);
      }
    },
    removeEventListener: (type, listener) => {
      target.removeEventListener(type, listener);
    },
    dispatchEvent: (event) => {
      if (event instanceof CustomEvent) {
        heard.push({ type: event.type, detail: event.detail });
      }

      return target.dispatchEvent(event);
    },
  };
  const state: Rig = {
    page,
    sent,
    cookie: options.cookie ?? false,
    rechecks: 0,
    timers: [],
    ended: 0,
    reloads: 0,
    looks: [],
    heard,
    signedIn: options.signedIn ?? false,
  };
  const bridge = startSlicerBridge({
    page,
    slicer: createSlicer(() => page),
    endSession: () => {
      state.ended += 1;
      state.signedIn = false;

      return Promise.resolve();
    },
    schedule: (run, ms) => {
      const entry = { ms, run };
      state.timers.push(entry);

      return () => {
        state.timers.splice(state.timers.indexOf(entry), 1);
      };
    },
    reload: () => {
      state.reloads += 1;
    },
    onLook: (look) => {
      state.looks.push(look);
    },
  });
  // The re-check is GET /principal through the app's client, so a cookie
  // that holds no person answers 401 and the client tells the bridge, as it
  // tells it of every 401.
  bridge?.watch(
    () => state.signedIn,
    () => {
      state.rechecks += 1;
      if (!state.cookie) bridge.refused();
      state.signedIn = state.cookie;

      return Promise.resolve(state.signedIn);
    },
  );

  return Object.assign(state, { bridge });
};

const api = (page: BridgeWindow) => {
  const v1 = page._prusaConnect_v1;
  const v2 = page._prusaConnect_v2;
  if (v1 === undefined || v2 === undefined) {
    throw new Error('the bridge installed no API');
  }

  return { v1, v2 };
};

describe('the page outside PrusaSlicer', () => {
  it('installs nothing and asks nothing', () => {
    const r = rig({ inSlicer: false });

    expect(r.bridge).toBeUndefined();
    expect(r.page._prusaConnect_v1).toBeUndefined();
    expect(r.page._prusaConnect_v2).toBeUndefined();
  });
});

describe('the page inside PrusaSlicer', () => {
  it('installs both API versions and asks for the configuration', () => {
    const r = rig();
    const { v1, v2 } = api(r.page);

    expect(Object.keys(v1).sort()).toEqual([
      'init',
      'logout',
      'requestCompatiblePrinter',
      'setAccessToken',
      'setSessionId',
    ]);
    expect(Object.keys(v2).sort()).toEqual([
      'init',
      'logout',
      'requestCompatiblePrinter',
    ]);
    expect(r.sent).toEqual([{ action: 'REQUEST_CONFIG' }]);
  });

  it('never passes the credential on to the page', async () => {
    const r = rig({ cookie: true });
    const { v1, v2 } = api(r.page);

    v2.init({ accessToken: 'pcl_A1_secret', sessionId: 's1', language: 'en' });
    v1.init({ accessToken: 'pcl_A1_secret' });
    v1.setAccessToken('pcl_A1_secret');
    await settle();

    expect(r.heard).toEqual([
      {
        type: 'slicerinit',
        detail: { apiVersion: 2, data: { language: 'en' } },
      },
      { type: 'slicerinit', detail: { apiVersion: 1, data: {} } },
    ]);
    expect(JSON.stringify(r.heard)).not.toContain('pcl_A1_secret');
    expect(document.cookie).not.toContain('pcl_A1_secret');
  });

  it('asks the core once the slicer set its cookie, and refreshes before it ends', async () => {
    const r = rig({ cookie: true });
    const { v2 } = api(r.page);

    // Two calls at once check the session once.
    v2.init({});
    v2.init({});
    await settle();

    expect(r.rechecks).toBe(1);
    expect(r.reloads).toBe(0);
    expect(r.timers.map((timer) => timer.ms)).toEqual([REFRESH_MS]);

    r.sent.length = 0;
    r.timers[0]?.run();
    r.timers[0]?.run();
    expect(r.sent).toEqual([{ action: 'REQUEST_CONFIG' }]);
  });

  it('waits on a first visit while the slicer has no page session', async () => {
    const r = rig();
    // The page's own first check met a 401 before the slicer paired.
    r.bridge?.refused();

    api(r.page).v2.init({});
    await settle();
    api(r.page).v2.init({});
    await settle();

    expect(r.bridge?.stale).toBe(false);
    expect(r.sent).toEqual([{ action: 'REQUEST_CONFIG' }]);
    expect(r.timers).toEqual([]);

    // The slicer paired and set its cookie: the page opens without a reload.
    r.cookie = true;
    api(r.page).v2.init({});
    await settle();
    expect(r.reloads).toBe(0);
  });

  it('asks again when the slicer never answered a REQUEST_CONFIG', () => {
    const r = rig({ signedIn: true });
    expect(r.sent).toEqual([{ action: 'REQUEST_CONFIG' }]);

    r.bridge?.refused();
    expect(r.sent).toHaveLength(1);

    r.timers.find((timer) => timer.ms === ASK_MS)?.run();
    r.bridge?.refused();
    expect(r.sent).toHaveLength(2);
  });

  it('fixes its two objects so no script on the page wraps them', () => {
    const r = rig();
    const { v2 } = api(r.page);
    const wrapped = (): undefined => undefined;

    expect(() => {
      Object.assign(r.page, { _prusaConnect_v2: { ...v2, init: wrapped } });
    }).toThrow();
    expect(() => {
      Object.assign(v2, { init: wrapped });
    }).toThrow();
    expect(api(r.page).v2).toBe(v2);
  });

  it('asks once after a 401, and reloads when the slicer opened a new page session', async () => {
    const r = rig({ signedIn: true, cookie: true });
    const { v2 } = api(r.page);
    // The configuration the page asked for when it started arrived.
    v2.init({});
    await settle();
    r.sent.length = 0;

    r.bridge?.refused();
    r.bridge?.refused();
    expect(r.sent).toEqual([{ action: 'REQUEST_CONFIG' }]);

    r.signedIn = false;
    v2.init({});
    await settle();
    expect(r.reloads).toBe(1);
    expect(r.bridge?.stale).toBe(false);

    // The refusal is settled: the next init reloads nothing.
    v2.init({});
    await settle();
    expect(r.reloads).toBe(1);
  });

  it('stops after a 401 when the slicer has nothing this server takes', async () => {
    const r = rig({ signedIn: true, cookie: false });
    const { v2 } = api(r.page);
    let told = 0;
    r.bridge?.onStale(() => {
      told += 1;
    });

    r.bridge?.refused();
    r.signedIn = false;
    v2.init({});
    await settle();
    v2.init({});
    await settle();

    expect(r.bridge?.stale).toBe(true);
    expect(told).toBe(1);
    expect(r.reloads).toBe(0);
  });

  it('sends the init event with the language this page reads', () => {
    const r = rig();

    api(r.page).v2.init({ language: 'cs_CZ', colorMode: 'DARK' });

    expect(r.heard).toEqual([
      {
        type: 'slicerinit',
        detail: { apiVersion: 2, data: { language: 'cs', colorMode: 'DARK' } },
      },
    ]);
    expect(r.looks).toEqual([{ language: 'cs', dark: true }]);
    expect(r.reloads).toBe(0);
  });

  it('swaps the two nozzle flags for version 1 and not for version 2', () => {
    const r = rig();
    const { v1, v2 } = api(r.page);
    const params = {
      printerModel: 'XL',
      filament_abrasive: [true, false],
      high_flow: [false, false],
    };

    v1.requestCompatiblePrinter(params);
    v2.requestCompatiblePrinter(params);

    expect(r.heard).toEqual([
      {
        type: 'requestcompatibleprinter',
        detail: {
          apiVersion: 1,
          data: {
            printerModel: 'XL',
            filament_abrasive: [false, false],
            high_flow: [true, false],
          },
        },
      },
      {
        type: 'requestcompatibleprinter',
        detail: { apiVersion: 2, data: params },
      },
    ]);
  });

  it('ends the page session on logout and reloads a signed-in page', async () => {
    const r = rig({ signedIn: true, cookie: true });

    api(r.page).v2.logout();
    await settle();

    expect(r.ended).toBe(1);
    expect(r.heard).toEqual([
      { type: 'logout', detail: { apiVersion: 2, data: null } },
    ]);
    expect(r.reloads).toBe(1);
  });

  it('opens a link to a new window in the system browser', () => {
    const r = rig();
    r.sent.length = 0;
    const outside = document.createElement('a');
    outside.href = '/docs/page';
    outside.target = '_blank';
    outside.textContent = 'docs';
    const inside = document.createElement('a');
    inside.href = '/printers';
    inside.textContent = 'printers';
    document.body.append(outside, inside);
    cleanups.push(() => {
      outside.remove();
      inside.remove();
    });

    const blank = new MouseEvent('click', { bubbles: true, cancelable: true });
    outside.dispatchEvent(blank);
    const same = new MouseEvent('click', { bubbles: true, cancelable: true });
    // Stopped here so the test page does not navigate.
    inside.addEventListener('click', (event) => {
      event.preventDefault();
    });
    inside.dispatchEvent(same);

    expect(blank.defaultPrevented).toBe(true);
    expect(r.sent).toEqual([
      {
        action: 'REQUEST_OPEN_IN_BROWSER',
        url: new URL('/docs/page', location.href).href,
      },
    ]);
  });
});

describe('a link to another scheme', () => {
  it('is never handed to the slicer', () => {
    const r = rig();
    r.sent.length = 0;
    const links = [
      'file:///etc/passwd',
      'javascript:void(0)',
      'smb://host/share',
      'prusaslicer://open?x=1',
    ].map((href) => {
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.target = '_blank';
      document.body.append(anchor);
      cleanups.push(() => {
        anchor.remove();
      });

      return anchor;
    });

    for (const anchor of links) {
      const click = new MouseEvent('click', {
        bubbles: true,
        cancelable: true,
      });
      anchor.dispatchEvent(click);
      expect(click.defaultPrevented).toBe(true);
    }

    expect(r.sent).toEqual([]);
  });
});
