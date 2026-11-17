import { describe, expect, it } from 'vitest';

import { searchOf, spotOf, TOP } from './navigation-query';
import { type Address, Navigation } from './navigation.svelte';

/** A browser with an address bar, a history and an event target, and nothing else. */
const fakeBrowser = (pathname = '/m/ns/parts', search = '') => {
  const location = { pathname, search };
  const pushed: string[] = [];
  const target = new EventTarget();
  let listening = 0;

  const address: Address = {
    location,
    history: {
      pushState: (_data, _unused, url) => {
        pushed.push(url);
        const at = new URL(url, 'http://test');
        location.pathname = at.pathname;
        location.search = at.search;
      },
    },
    addEventListener: (type, listener) => {
      listening += 1;
      target.addEventListener(type, listener);
    },
    removeEventListener: (type, listener) => {
      listening -= 1;
      target.removeEventListener(type, listener);
    },
    dispatchEvent: (event) => target.dispatchEvent(event),
  };

  return {
    address,
    location,
    pushed,
    listening: () => listening,
    back: (url: string) => {
      const at = new URL(url, 'http://test');
      location.pathname = at.pathname;
      location.search = at.search;
      target.dispatchEvent(new Event('popstate'));
    },
  };
};

describe('the place in the address', () => {
  it('reads every field it writes', () => {
    const spot = { folder: 4, part: 12, q: 'hook', tag: 'garage' };

    expect(spotOf(searchOf(spot))).toEqual(spot);
    expect(searchOf(TOP)).toBe('');
  });

  it('leaves out what it cannot read rather than guessing', () => {
    expect(spotOf('?folder=-1&part=abc&q=')).toEqual(TOP);
    expect(spotOf('?folder=99999999999')).toEqual(TOP);
  });

  it('cuts a search to the length the module answers', () => {
    expect(spotOf(`?q=${'a'.repeat(500)}`).q).toHaveLength(120);
  });
});

describe('Navigation', () => {
  it('starts at the top until a page attaches', () => {
    const browser = fakeBrowser('/m/ns/parts', '?part=3');
    const navigation = new Navigation(browser.address);

    expect(navigation.spot).toEqual(TOP);

    navigation.attach();
    expect(navigation.spot.part).toBe(3);
  });

  it('writes a move into the history and tells the router', () => {
    const browser = fakeBrowser();
    const navigation = new Navigation(browser.address);
    navigation.attach();

    let routerHeard = 0;
    browser.address.addEventListener('popstate', () => {
      routerHeard += 1;
    });

    navigation.openFolder(7);

    expect(browser.pushed).toEqual(['/m/ns/parts?folder=7']);
    expect(routerHeard).toBe(1);
    expect(navigation.spot.folder).toBe(7);
  });

  it('writes nothing for a move to the place already on screen', () => {
    const browser = fakeBrowser('/m/ns/parts', '?folder=7');
    const navigation = new Navigation(browser.address);
    navigation.attach();

    navigation.openFolder(7);

    expect(browser.pushed).toEqual([]);
  });

  it('follows the back button and tells each listener once', () => {
    const browser = fakeBrowser();
    const navigation = new Navigation(browser.address);
    navigation.attach();

    const heard: number[] = [];
    navigation.onMove((spot) => heard.push(spot.folder));

    browser.back('/m/ns/parts?folder=2');
    browser.back('/m/ns/parts?folder=2');

    expect(heard).toEqual([2]);
  });

  it('ignores an address that is another screen', () => {
    const browser = fakeBrowser('/m/ns/parts', '?folder=2');
    const navigation = new Navigation(browser.address);
    navigation.attach();

    browser.back('/printers/abc?folder=9');

    expect(navigation.spot.folder).toBe(2);
  });

  it('lets go of every listener it added when the page detaches', () => {
    const browser = fakeBrowser();
    const navigation = new Navigation(browser.address);

    for (let round = 0; round < 5; round += 1) {
      const detach = navigation.attach();
      detach();
    }

    expect(browser.listening()).toBe(0);
  });

  it('stops telling a listener that stopped listening', () => {
    const browser = fakeBrowser();
    const navigation = new Navigation(browser.address);
    navigation.attach();

    let heard = 0;
    const stop = navigation.onMove(() => {
      heard += 1;
    });
    stop();
    navigation.openFolder(3);

    expect(heard).toBe(0);
  });
});
