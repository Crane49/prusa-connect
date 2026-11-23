import { describe, expect, it } from 'vitest';

import { createSlicer, type SlicerMessage, type SlicerWindow } from './slicer';

const pageWith = (
  host?: SlicerWindow['_prusaSlicer'],
): SlicerWindow & { sent: SlicerMessage[] } => {
  const target = new EventTarget();
  const sent: SlicerMessage[] = [];
  const page: SlicerWindow & { sent: SlicerMessage[] } = {
    sent,
    _prusaSlicer: host,
    addEventListener: (type, listener) => {
      target.addEventListener(type, listener);
    },
    removeEventListener: (type, listener) => {
      target.removeEventListener(type, listener);
    },
    dispatchEvent: (event) => target.dispatchEvent(event),
  };

  return page;
};

describe('the slicer helper', () => {
  it('is not available on a page the slicer did not open', () => {
    const page = pageWith();
    const slicer = createSlicer(() => page);

    expect(slicer.available).toBe(false);
    expect(slicer.post({ action: 'REQUEST_CONFIG' })).toBe(false);
  });

  it('is not available where there is no window', () => {
    const slicer = createSlicer(() => undefined);

    expect(slicer.available).toBe(false);
    expect(slicer.post({ action: 'CLOSE_DIALOG' })).toBe(false);
    // Nothing to listen on, and stopping it is still safe.
    slicer.on('logout', () => undefined)();
  });

  it('is not available when the slicer object has no postMessage', () => {
    const slicer = createSlicer(() => pageWith({}));

    expect(slicer.available).toBe(false);
  });

  it('sends a message to the slicer as it was given', () => {
    const sent: SlicerMessage[] = [];
    const page = pageWith({ postMessage: (message) => sent.push(message) });
    const slicer = createSlicer(() => page);

    expect(slicer.available).toBe(true);
    expect(
      slicer.post({ action: 'REQUEST_OPEN_IN_BROWSER', url: 'https://x' }),
    ).toBe(true);
    expect(sent).toEqual([
      { action: 'REQUEST_OPEN_IN_BROWSER', url: 'https://x' },
    ]);
  });

  it('hands a listener the data and the API version of an event', () => {
    const page = pageWith({ postMessage: () => undefined });
    const slicer = createSlicer(() => page);
    const heard: [unknown, number][] = [];
    const stop = slicer.on('requestcompatibleprinter', (data, version) => {
      heard.push([data, version]);
    });

    slicer.emit('requestcompatibleprinter', { printerModel: 'MK4S' }, 2);
    stop();
    slicer.emit('requestcompatibleprinter', { printerModel: 'XL' }, 2);

    expect(heard).toEqual([[{ printerModel: 'MK4S' }, 2]]);
  });

  it('writes the detail of a slicer event', () => {
    const page = pageWith();
    const slicer = createSlicer(() => page);
    const details: unknown[] = [];
    page.addEventListener('slicerinit', (event) => {
      if (event instanceof CustomEvent) details.push(event.detail);
    });

    slicer.emit('slicerinit', { language: 'cs' });

    expect(details).toEqual([{ apiVersion: 1, data: { language: 'cs' } }]);
  });

  it('ignores an event of the same name with another shape', () => {
    const page = pageWith();
    const slicer = createSlicer(() => page);
    let heard = 0;
    slicer.on('logout', () => {
      heard += 1;
    });

    page.dispatchEvent(new Event('logout'));
    page.dispatchEvent(new CustomEvent('logout', { detail: 'x' }));
    slicer.emit('logout', null);

    expect(heard).toBe(1);
  });
});
