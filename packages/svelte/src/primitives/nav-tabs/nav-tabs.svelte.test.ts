import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import NavTabs from './nav-tabs.svelte';

const tabs = [
  { id: 'general', href: '/general', label: 'General' },
  { id: 'network', href: '/network', label: 'Network' },
];

describe('NavTabs', () => {
  it('marks only the open tab as the current page', async () => {
    await render(NavTabs, {
      props: { label: 'Settings', tabs, active: 'network' },
    });

    const current = [...document.querySelectorAll('a')].filter(
      (a) => a.getAttribute('aria-current') === 'page',
    );
    expect(current.map((a) => a.textContent.trim())).toEqual(['Network']);
    expect(document.querySelectorAll('a[aria-current]')).toHaveLength(1);
  });

  it('has no accessibility violations', async () => {
    const { container } = await render(NavTabs, {
      props: { label: 'Settings', tabs, active: 'general' },
    });
    await expectNoViolations(container);
  });
});
