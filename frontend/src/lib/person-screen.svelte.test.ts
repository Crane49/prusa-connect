// The screen the app draws belongs to the person signed in. When another
// person signs in without a reload, the screen is mounted fresh for them.
import { createRawSnippet, flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import PersonScreen from './person-screen.svelte';

describe('the screen drawn for a person', () => {
  it('mounts the screen again when the person changes, and only then', async () => {
    let mounts = 0;
    const children = createRawSnippet(() => ({
      render: () => '<p>screen</p>',
      setup: () => {
        mounts += 1;
      },
    }));

    const props = $state({ person: 'user:ada', diagnostics: true, children });
    await render(PersonScreen, props);
    expect(mounts).toBe(1);

    props.person = 'user:ada';
    flushSync();
    expect(mounts).toBe(1);

    props.person = 'user:grace';
    flushSync();
    expect(mounts).toBe(2);
  });
});
