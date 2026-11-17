import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';

import { expectNoViolations } from './axe';

// The helper itself, proved against a control it must refuse. A gate written
// against a clean page reports nothing and proves nothing, so the failing case
// is the test that matters here.
const mount = (html: string): HTMLElement => {
  const host = document.createElement('div');
  host.innerHTML = html;
  page.elementLocator(document.body).element().append(host);
  return host;
};

describe('the axe helper', () => {
  it('passes an element with nothing wrong with it', async () => {
    await expectNoViolations(mount('<button type="button">Print</button>'));
  });

  it('names the rule and the element when a control has no name', async () => {
    const host = mount('<button type="button"></button>');

    await expect(expectNoViolations(host)).rejects.toThrow(/button-name/);
  });
});
