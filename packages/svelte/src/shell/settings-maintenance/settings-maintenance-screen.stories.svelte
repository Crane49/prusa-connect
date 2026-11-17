<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { pairingSource, phaseSource } from '../portal-pairing/story-source';
  import SettingsMaintenanceScreen from './settings-maintenance-screen.svelte';
  import type { MaintenanceSource } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Maintenance',
    component: SettingsMaintenanceScreen,
  });

  const sourceOf = (
    over: Partial<MaintenanceSource> = {},
  ): MaintenanceSource => ({
    version: {
      build_profile: 'server',
      build_variant: 'full',
      capabilities: [],
      contract: '1.0.0',
      goarch: 'arm64',
      goos: 'linux',
      host_schema_compat: 10,
      host_schema_version: 12,
      managed_externally: false,
      version: '1.2.0',
    },
    upgrade: undefined,
    releases: [],
    releaseError: undefined,
    releaseRetryAt: undefined,
    releaseLoading: false,
    history: [],
    backups: [],
    backupError: undefined,
    backupLoading: false,
    deliveryAvailable: true,
    portal: pairingSource(),
    offlineBundle: undefined,
    offlineUploadProgress: undefined,
    loaded: true,
    loading: false,
    busy: undefined,
    error: undefined,
    refreshReleases: () => Promise.resolve(),
    refreshBackups: () => Promise.resolve(),
    uploadOfflineBundle: () => Promise.resolve(),
    stage: () => Promise.resolve(),
    prepare: () => Promise.resolve(),
    apply: () => Promise.resolve(),
    cancel: () => Promise.resolve(),
    createBackup: () => Promise.resolve(),
    restoreBackup: () => Promise.resolve(),
    deleteBackup: () => Promise.resolve(),
    ...over,
  });
</script>

<Story name="Online Update" asChild>
  <SettingsMaintenanceScreen
    source={sourceOf({
      portal: phaseSource('paired'),
      releases: [
        {
          binary: {
            name: 'prusa-connect_linux_arm64',
            size: 42_000_000,
            url: '/release/core',
          },
          name: 'Prusa Connect 1.3.0',
          notes: 'Reliability improvements and safer recovery.',
          prerelease: false,
          published_at: '2026-10-25T19:14:05Z',
          signature: {
            name: 'SHA256SUMS.sig',
            size: 96,
            url: '/release/signature',
          },
          sums: {
            name: 'SHA256SUMS',
            size: 64,
            url: '/release/sums',
          },
          version: '1.3.0',
        },
      ],
    })}
  />
</Story>

<Story name="Offline Bundle" asChild>
  <SettingsMaintenanceScreen
    source={sourceOf({
      offlineUploadProgress: 100,
      offlineBundle: {
        name: 'Prusa Connect 1.3.0',
        version: '1.3.0',
        artifacts: [
          {
            kind: 'core',
            name: 'prusa-connect',
            version: '1.3.0',
            size: 42_000_000,
          },
          {
            kind: 'extension',
            name: 'fleet-tools',
            version: '2.1.0',
            size: 3_200_000,
          },
        ],
        impacts: [
          {
            extension: 'camera-bridge',
            outcome: 'disabled',
            reason: 'This extension requires an older host contract.',
          },
        ],
      },
      upgrade: {
        from_version: '1.2.0',
        id: 'offline-1',
        revision: 1,
        schema: 1,
        staged_binary: '/updates/candidate',
        state: 'staged',
        to_version: '1.3.0',
        updated_at: '2026-10-26T12:00:00Z',
      },
    })}
  />
</Story>

<Story name="Delivery Unavailable" asChild>
  <SettingsMaintenanceScreen
    source={sourceOf({
      deliveryAvailable: false,
      portal: undefined,
    })}
  />
</Story>
