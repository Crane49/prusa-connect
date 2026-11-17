<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SettingsUpdatesScreen from './settings-updates-screen.svelte';
  import { phaseSource } from '../portal-pairing/story-source';
  import {
    maintenanceSource,
    preparedUpgrade,
    reviewsNeedingApproval,
  } from './story-source';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Updates',
    component: SettingsUpdatesScreen,
  });
</script>

<Story name="Disconnected" asChild>
  <SettingsUpdatesScreen source={maintenanceSource()} />
</Story>

<Story name="Pairing" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({ portal: phaseSource('code') })}
  />
</Story>

<Story name="Paired" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({ portal: phaseSource('paired') })}
  />
</Story>

<Story name="Offline" asChild>
  <SettingsUpdatesScreen source={maintenanceSource()} method="offline" />
</Story>

<Story name="Extensions Need Approval" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({
      upgrade: preparedUpgrade({
        extension_reviews: reviewsNeedingApproval,
      }),
    })}
    method="offline"
  />
</Story>

<Story name="Extensions Reviewed" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({
      upgrade: preparedUpgrade({
        extension_reviews: [
          {
            module: 'github.com/example/fleet-tools',
            version: '2.1.0',
            installed_version: '2.0.0',
            asks: ['read printers'],
          },
          {
            module: 'github.com/example/label-printer',
            version: '0.3.0',
          },
        ],
      }),
    })}
    method="offline"
  />
</Story>

<Story name="Rolled Back With Databases To Restore" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({
      upgrade: preparedUpgrade({
        state: 'rolled-back',
        failure: 'the candidate did not start within 90 seconds',
        external_restore: ['prusa_connect', 'farm_module'],
      }),
      history: [
        preparedUpgrade({
          id: 'online-0',
          from_version: '1.1.0',
          to_version: '1.2.0',
          state: 'rolled-back',
          updated_at: '2026-10-20T08:30:00Z',
          external_restore: ['prusa_connect'],
        }),
      ],
    })}
  />
</Story>

<Story name="Update Server Limited" asChild>
  <SettingsUpdatesScreen
    source={maintenanceSource({
      portal: phaseSource('paired'),
      releaseError: {
        code: 'core.resource/UPGRADE_SERVER_LIMITED',
        message: 'the update server asks to wait 300 seconds',
        params: { retry_after_seconds: 300 },
        status: 429,
        traceId: 'limited-trace',
      },
      releaseRetryAt: Date.now() + 300_000,
    })}
  />
</Story>
