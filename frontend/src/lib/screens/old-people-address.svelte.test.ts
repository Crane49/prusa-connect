// A bookmark to /settings/users opens the People room.
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { PATHS } from '../routes';
import OldPeopleAddress from './old-people-address.svelte';

describe('the old address of the People room', () => {
  it('keeps the query and the fragment of the old address', async () => {
    const went: string[] = [];
    const ctx = appContext(
      {},
      {
        search: '?view=table',
        hash: '#demo',
        go: (path) => {
          went.push(path);
        },
      },
    );

    await render(OldPeopleAddress, { props: { ctx } });

    await expect
      .poll(() => went)
      .toEqual([`${PATHS.settingsPeople}?view=table#demo`]);
  });

  it('moves to the People room without a history entry', async () => {
    const went: { path: string; replace: boolean | undefined }[] = [];
    const ctx = appContext(
      {},
      {
        go: (path, options) => {
          went.push({ path, replace: options?.replace });
        },
      },
    );

    await render(OldPeopleAddress, { props: { ctx } });

    await expect
      .poll(() => went)
      .toEqual([{ path: PATHS.settingsPeople, replace: true }]);
  });
});
