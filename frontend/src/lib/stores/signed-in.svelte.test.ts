// The work the app starts for a signed-in person, and what happens to it when
// a different person signs in without the page reloading.
import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { forgetEachPerson, whileSignedIn } from './signed-in.svelte';

describe('the work started for a signed-in person', () => {
  it('starts again for a new person, and stops for the old one first', () => {
    const session = $state<{
      status: string;
      principal: { id: string } | undefined;
    }>({ status: 'signed-in', principal: { id: 'user:ada' } });
    const log: string[] = [];

    const cleanup = $effect.root(() => {
      whileSignedIn(session, () => {
        const person = session.principal?.id ?? 'nobody';
        log.push(`start ${person}`);

        return () => {
          log.push(`stop ${person}`);
        };
      });
    });

    flushSync();
    expect(log).toEqual(['start user:ada']);

    // A session read again hands over a new object for the same person.
    session.principal = { id: 'user:ada' };
    flushSync();
    expect(log).toEqual(['start user:ada']);

    session.principal = { id: 'user:grace' };
    flushSync();
    expect(log).toEqual([
      'start user:ada',
      'stop user:ada',
      'start user:grace',
    ]);

    session.status = 'signed-out';
    session.principal = undefined;
    flushSync();
    expect(log.at(-1)).toBe('stop user:grace');

    cleanup();
  });
});

describe('forgetting what the last person read', () => {
  it('forgets when a person leaves, before the work starts again', () => {
    const session = $state<{
      status: string;
      principal: { id: string } | undefined;
    }>({ status: 'signed-in', principal: { id: 'user:ada' } });
    const log: string[] = [];

    const cleanup = $effect.root(() => {
      whileSignedIn(session, () => {
        log.push(`start ${session.principal?.id ?? 'nobody'}`);

        return () => undefined;
      });
      forgetEachPerson(session, () => {
        log.push('forget');
      });
    });

    flushSync();
    expect(log).toEqual(['start user:ada']);

    // A session read again for the same person forgets nothing.
    session.principal = { id: 'user:ada' };
    flushSync();
    expect(log).toEqual(['start user:ada']);

    // The work for the new person starts after the forgetting, so nothing
    // the new person reads is forgotten.
    session.principal = { id: 'user:grace' };
    flushSync();
    expect(log).toEqual(['start user:ada', 'forget', 'start user:grace']);

    session.status = 'signed-out';
    session.principal = undefined;
    flushSync();
    expect(log.at(-1)).toBe('forget');

    // A sign-in after a sign-out has nobody before it to forget.
    const forgotten = log.filter((line) => line === 'forget').length;
    session.status = 'signed-in';
    session.principal = { id: 'user:ada' };
    flushSync();
    expect(log.filter((line) => line === 'forget')).toHaveLength(forgotten);

    cleanup();
  });
});
