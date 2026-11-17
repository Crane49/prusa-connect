<!--
  The page of one print, over the story core.
  States: a finished print (the recorded job), a running print, a print that
  failed, a cancelled print, a job id the core does not know, loading, the
  read of the job failed, refused.
  The core's job table holds finished prints only, so the other states answer
  the finished job's row with another state.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import fixtures from '../../../test/ux/fixtures/api.json';
  import { json, problem } from '../../../src/test/stub';
  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { PATHS } from '../routes';

  const { Story } = defineMeta({ title: 'Screens/Jobs/Job detail' });

  const ID = '7cdb6b60-86a8-4840-82b2-ce3927435df8';
  const KEY = `GET /api/v1/jobs/${ID}`;
  const at = `${PATHS.jobs}/${ID}`;

  const recorded = JSON.parse(fixtures[KEY].body) as Record<string, unknown>;

  const as = (row: Record<string, unknown>) => ({
    [KEY]: () => json(200, { ...recorded, ...row }),
  });
</script>

<Story name="Finished"><AppAt path={at} /></Story>
<Story name="Running">
  <AppAt
    path={at}
    overrides={as({
      state: 'PRINTING',
      progress: 42,
      ended: undefined,
      time_remaining_ms: 1_740_000,
    })}
  />
</Story>
<Story name="Print failed">
  <AppAt path={at} overrides={as({ state: 'ERROR', progress: 63 })} />
</Story>
<Story name="Cancelled">
  <AppAt path={at} overrides={as({ state: 'STOPPED', progress: 12 })} />
</Story>
<Story name="Missing">
  <AppAt
    path={`${PATHS.jobs}/00000000-0000-4000-8000-000000000000`}
    overrides={{
      'GET /api/v1/jobs/00000000-0000-4000-8000-000000000000': () =>
        problem(404, 'core.api/NOT_FOUND'),
    }}
  />
</Story>
<Story name="Loading"><AppAt path={at} overrides={{ [KEY]: pending }} /></Story>
<Story name="Failed"><AppAt path={at} overrides={{ [KEY]: failed }} /></Story>
<Story name="Refused"><AppAt path={at} overrides={{ [KEY]: refused }} /></Story>
