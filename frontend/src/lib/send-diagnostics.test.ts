import { describe, expect, it } from 'vitest';

import {
  type PortalTab,
  portalPage,
  sendDiagnosticsToPortal,
  type SendSteps,
} from './send-diagnostics';

const PAGE = 'https://beta.example/prusa-connect/diagnostics/abc';

const tab = (): PortalTab & { closed: boolean } => {
  const opened: PortalTab & { closed: boolean } = {
    opener: {},
    location: { href: 'about:blank' },
    closed: false,
    close: () => {
      opened.closed = true;
    },
  };

  return opened;
};

const steps = (over: Partial<SendSteps>, order: string[]): SendSteps => ({
  open: () => {
    order.push('open');

    return tab();
  },
  build: () => {
    order.push('build');

    return Promise.resolve(new Blob(['PK']));
  },
  version: () => Promise.resolve('0.0.1'),
  upload: () => {
    order.push('upload');

    return Promise.resolve({ url: PAGE });
  },
  go: () => {
    order.push('go');
  },
  ...over,
});

describe('sendDiagnosticsToPortal', () => {
  it('opens the tab first, then points it at the portal page with the intent', async () => {
    const order: string[] = [];
    const opened = tab();

    await sendDiagnosticsToPortal(
      'existing',
      steps(
        {
          open: () => {
            order.push('open');

            return opened;
          },
        },
        order,
      ),
    );

    expect(order).toEqual(['open', 'build', 'upload']);
    expect(opened.opener).toBeNull();
    expect(opened.location.href).toBe(`${PAGE}?intent=existing`);
  });

  it('closes the tab and throws when the upload fails', async () => {
    const opened = tab();
    const failure = new Error('refused');

    await expect(
      sendDiagnosticsToPortal(
        'new',
        steps(
          { open: () => opened, upload: () => Promise.reject(failure) },
          [],
        ),
      ),
    ).rejects.toBe(failure);
    expect(opened.closed).toBe(true);
  });

  it('shows the page here when the browser opened no tab', async () => {
    const went: string[] = [];

    await sendDiagnosticsToPortal(
      'new',
      steps(
        {
          open: () => null,
          go: (url) => {
            went.push(url);
          },
        },
        [],
      ),
    );

    expect(went).toEqual([`${PAGE}?intent=new`]);
  });
});

describe('portalPage', () => {
  it('adds the intent to an address that already has a query', () => {
    expect(portalPage(`${PAGE}?lang=cs`, 'new')).toBe(
      `${PAGE}?lang=cs&intent=new`,
    );
  });
});
