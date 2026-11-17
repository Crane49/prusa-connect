<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { Device } from '../../domain';
  import DashboardScreen from './dashboard-screen.svelte';
  import { dashboardContextOver, fakeCore, storedDashboard } from './fixtures';
  import { fakeDevice } from '../../test/fakes';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Dashboard',
    component: DashboardScreen,
  });

  const at = (id: string, name: string, state: string): Device => ({
    ...fakeDevice(id, name),
    state: { '/state': state },
  });

  const FLEET: readonly Device[] = [
    {
      ...at('core', 'CORE One bench', 'PRINTING'),
      job: {
        display_name: 'bracket.bgcode',
        id: '00000000-0000-4000-8000-000000000001',
        device_job_id: '1',
        path: 'usb/bracket.bgcode',
        progress: 64,
        running: true,
        state: 'PRINTING',
        time_remaining_ms: 42 * 60_000,
      },
    },
    at('mk4', 'MK4S garage', 'IDLE'),
    at('mini', 'MINI desk', 'ATTENTION'),
    at('mk3', 'MK3S shelf', 'ERROR'),
  ];

  const card = (
    id: string,
    type: string,
    [x, y, w, h]: readonly [number, number, number, number],
    config: Record<string, unknown> = {},
  ) => ({ id, type, x, y, w, h, title: '', config });

  const WALL = storedDashboard(4, 'Workshop wall', [
    card('s1', 'core/summary', [0, 0, 8, 2]),
    card('c1', 'core/clock', [8, 0, 4, 2]),
    card('p1', 'core/printers', [0, 2, 8, 5]),
    card('i1', 'core/issues', [8, 2, 4, 5]),
    card('f1', 'core/finishing', [0, 7, 6, 4]),
    card('n1', 'core/note', [6, 7, 6, 4], {
      text: 'Ask before you take a plate',
    }),
    card('x1', 'spools/left', [0, 11, 4, 2]),
  ]);

  const OFFICE = storedDashboard(7, 'Office', []);

  const LONG = storedDashboard(
    9,
    'Second shift wall for the north hall, every printer and every camera',
    WALL.cards,
  );

  const contextOf = (
    dashboards: Parameters<typeof fakeCore>[0],
    devices: readonly Device[] = FLEET,
    reads: 'answer' | 'fail' | 'wait' = 'answer',
  ) => {
    const { client } = fakeCore(dashboards, { defaultId: 4 });
    const listDashboards = {
      answer: client.listDashboards,
      fail: () =>
        Promise.reject(
          Object.assign(new Error('unreachable'), { status: 503 }),
        ),
      wait: () => new Promise<never>(() => undefined),
    }[reads];

    return dashboardContextOver({ ...client, listDashboards }, devices).context;
  };
</script>

<!--
  The dashboard screen over a fake core: the states it draws as itself.
-->

<Story name="Default Dashboard" asChild>
  <DashboardScreen context={contextOf([WALL, OFFICE])} />
</Story>

<Story name="Editing" asChild>
  <DashboardScreen context={contextOf([WALL, OFFICE])} id={4} edit />
</Story>

<Story name="Empty Dashboard" asChild>
  <DashboardScreen context={contextOf([WALL, OFFICE])} id={7} />
</Story>

<Story name="Written From The Fleet" asChild>
  <DashboardScreen context={contextOf([])} />
</Story>

<Story name="No Printer Yet" asChild>
  <DashboardScreen context={contextOf([], [])} />
</Story>

<Story name="Kiosk" asChild>
  <DashboardScreen context={contextOf([WALL])} id={4} kiosk />
</Story>

<Story name="Gone" asChild>
  <DashboardScreen context={contextOf([WALL])} id={99} />
</Story>

<Story name="List Unreadable" asChild>
  <DashboardScreen context={contextOf([WALL], FLEET, 'fail')} />
</Story>

<!-- A name longer than the header at phone width ends in an ellipsis, and
     the switcher's title holds the whole of it. -->
<Story name="Long Name At Phone Width" asChild>
  <div class="max-w-90">
    <DashboardScreen context={contextOf([LONG, WALL])} id={9} />
  </div>
</Story>

<Story name="Loading" asChild>
  <DashboardScreen context={contextOf([WALL], FLEET, 'wait')} />
</Story>
