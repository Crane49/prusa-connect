<!--
  The jobs screen, all printers and one printer, over the story core.
  States: populated (every printer's history), one printer's jobs, no jobs
  anywhere, loading, failed, refused. The history read is per printer, so each
  state answers the history of every printer of the chosen fleet.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fleetOf } from '../../../.storybook/fleet';
  import {
    emptyList,
    failed,
    type Overrides,
    pending,
    refused,
  } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { jobsOfPrinterHref, PATHS } from '../routes';

  const { Story } = defineMeta({ title: 'Screens/Jobs' });

  // The history of every printer of the largest fleet, so a smaller fleet is
  // covered too.
  const everyHistory = (answer: Overrides[string]): Overrides =>
    Object.fromEntries(
      fleetOf('many').map((device) => [
        `GET /api/v1/devices/${device.id}/jobs/history`,
        answer,
      ]),
    );

  const first = fleetOf('one')[0]?.id ?? '';
</script>

<Story name="All printers"><AppAt path={PATHS.jobs} /></Story>
<Story name="One printer">
  <AppAt path={jobsOfPrinterHref(first)} />
</Story>
<Story name="Empty">
  <AppAt path={PATHS.jobs} overrides={everyHistory(emptyList)} />
</Story>
<Story name="Loading">
  <AppAt path={PATHS.jobs} overrides={everyHistory(pending)} />
</Story>
<Story name="Failed">
  <AppAt path={PATHS.jobs} overrides={everyHistory(failed)} />
</Story>
<Story name="Refused">
  <AppAt path={PATHS.jobs} overrides={everyHistory(refused)} />
</Story>
