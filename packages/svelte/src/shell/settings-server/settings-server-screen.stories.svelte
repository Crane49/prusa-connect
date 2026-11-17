<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SettingsServerScreen from './settings-server-screen.svelte';
  import type { HealthSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Server',
    component: SettingsServerScreen,
  });

  const READY = {
    app: 'prusa-connect',
    blocked: null,
    declared: 4,
    instances: [],
    ready: true,
    serving: 4,
  };

  const sourceOf = (over: Partial<HealthSource> = {}): HealthSource => ({
    readiness: READY,
    connectors: [
      {
        connector: 'prusalink',
        counts: { connected: 3 },
        degraded: null,
        degraded_truncated: 0,
        status: 'pass',
        total: 3,
      },
    ],
    readinessError: undefined,
    connectorsError: undefined,
    loaded: true,
    ...over,
  });

  const FAULT = {
    code: 'core.transport/NETWORK',
    status: undefined,
    params: {},
    message: 'the server could not be reached',
    traceId: undefined,
  };
</script>

<Story name="Healthy" asChild>
  <SettingsServerScreen source={sourceOf()} />
</Story>

<Story name="Not Ready" asChild>
  <SettingsServerScreen
    source={sourceOf({
      readiness: {
        ...READY,
        ready: false,
        serving: 2,
        blocked: ['module/fleet-stats', 'connector/prusalink'],
      },
    })}
  />
</Story>

<Story name="Degraded Connector" asChild>
  <SettingsServerScreen
    source={sourceOf({
      connectors: [
        {
          connector: 'prusalink',
          counts: { connected: 5, error: 2, disconnected: 1 },
          degraded: [
            {
              id: 'printer-3',
              attempts: 4,
              reason: 'connection refused',
              since: '2026-09-23T09:00:00.000Z',
              state: 'error',
            },
            {
              id: 'printer-7',
              name: 'Workshop MK4',
              attempts: 0,
              last_seen: '2026-09-23T08:30:00.000Z',
              state: 'disconnected',
            },
          ],
          degraded_truncated: 1,
          status: 'degraded',
          total: 8,
        },
      ],
    })}
  />
</Story>

<Story name="Optional Instances Idle" asChild>
  <SettingsServerScreen
    source={sourceOf({
      readiness: {
        ...READY,
        serving: 3,
        instances: [
          {
            resource: 'connector:prusalink',
            state: 'running',
            required: true,
            serving: true,
          },
          {
            resource: 'module:fleet-stats',
            state: 'stopped',
            required: false,
            serving: false,
          },
        ],
      },
    })}
  />
</Story>

<Story name="No Connectors" asChild>
  <SettingsServerScreen source={sourceOf({ connectors: [] })} />
</Story>

<Story name="Both Reads Refused" asChild>
  <SettingsServerScreen
    source={sourceOf({
      readiness: undefined,
      connectors: [],
      readinessError: FAULT,
      connectorsError: FAULT,
    })}
  />
</Story>

<Story name="Still Reading" asChild>
  <SettingsServerScreen
    source={sourceOf({ readiness: undefined, connectors: [], loaded: false })}
  />
</Story>
