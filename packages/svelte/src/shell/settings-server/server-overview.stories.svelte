<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { PAIRING_NEEDED_FAULT } from '../../data';
  import { overviewOf } from './overview-fixtures';
  import ServerOverview from './server-overview.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Server Overview',
    component: ServerOverview,
  });

  const props = overviewOf();
</script>

<Story name="Printers Away" asChild>
  <ServerOverview {...props} />
</Story>

<Story name="All Quiet" asChild>
  <ServerOverview
    {...props}
    health={{
      ...props.health,
      readiness: props.health.readiness && {
        ...props.health.readiness,
        serving: 11,
      },
      connectors: [
        {
          connector: 'xbuddy',
          counts: { connected: 3 },
          degraded: [],
          degraded_truncated: 0,
          status: 'pass',
          total: 3,
        },
      ],
    }}
    maintenance={{ ...props.maintenance, releases: [] }}
  />
</Story>

<Story name="Still Reading" asChild>
  <ServerOverview
    {...props}
    health={{ ...props.health, loaded: false }}
    maintenance={{ ...props.maintenance, loaded: false }}
    network={{ ...props.network, loaded: false }}
  />
</Story>

<Story name="Inside A Bridged Container" asChild>
  <ServerOverview
    {...props}
    network={{
      ...props.network,
      described: {
        hidden: false,
        announcement: 'announcing',
        host: '19f634755a5c.local',
        port: 8080,
        bridged: true,
      },
    }}
  />
</Story>

<Story name="Updates Off" asChild>
  <ServerOverview
    {...props}
    updatesHref={undefined}
    backupsHref={undefined}
    maintenance={{ ...props.maintenance, releases: [], backups: [] }}
  />
</Story>

<Story name="Not Paired" asChild>
  <ServerOverview
    {...props}
    maintenance={{
      ...props.maintenance,
      releases: [],
      releaseError: {
        code: PAIRING_NEEDED_FAULT,
        message: 'pair this server with the Beta Portal first',
        params: {},
        status: 409,
        traceId: 'pairing-trace',
      },
    }}
  />
</Story>
