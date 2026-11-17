// Lines a chunk adds to a namespace join the ones it holds.
import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { locale } from './locale.svelte';
import { t } from './translate';

describe('the locale store', () => {
  it('keeps what a namespace holds when lines are added to it', () => {
    locale.register('test.extend', 'en', { 'test.extend.held': 'Held' });
    locale.extend('test.extend', 'en', { 'test.extend.added': 'Added' });

    expect(locale.message('test.extend', 'test.extend.held')).toBe('Held');
    expect(locale.message('test.extend', 'test.extend.added')).toBe('Added');

    locale.forget('test.extend');
  });

  it('ignores a language it has no catalogue for', () => {
    locale.extend('test.extend', 'xx', { 'test.extend.lost': 'Lost' });

    expect(locale.message('test.extend', 'test.extend.lost')).toBeUndefined();
  });

  it('records a missing key that a derived value reads, without failing it', () => {
    const key = 'test.locale.nowhere';
    let drawn = '';

    const stop = $effect.root(() => {
      const line = $derived(t(key));
      $effect(() => {
        drawn = line;
      });
    });
    flushSync();
    stop();

    expect(drawn).toBe(key);
    expect(locale.missing).toContain(key);
  });
});
