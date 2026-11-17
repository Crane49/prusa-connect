<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { instanceOf, rowOf } from '../../../../test/extension-fixtures';
  import SwitchedOffAlerts from './switched-off-alerts.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Switched Off Alerts',
    component: SwitchedOffAlerts,
  });

  const at = '2026-10-28T09:30:00Z';
  const offRow = (held: boolean) =>
    rowOf(
      {},
      {
        enabled: false,
        status: {
          ...instanceOf().status,
          state: 'failed',
          memory: {
            budget_bytes: 134_217_728,
            budget_source: 'default',
            failures_in_window: 3,
            held_until_restart: held,
            near_budget: false,
            on_over_memory: 'stop',
            over_budget: false,
            switch_off_failures: 3,
            switch_off_source: 'default',
            switch_off_window_seconds: 3600,
            switched_off: {
              at,
              stops: [
                { at, result: 'over-memory' },
                { at, result: 'over-memory' },
                { at, result: 'over-memory' },
              ],
            },
          },
        },
      },
    );
  const href = () => '/settings/extensions/com.github.prusa3d.spool-tracker';
</script>

<Story name="Switched Off" asChild>
  <SwitchedOffAlerts rows={[offRow(false)]} {href} />
</Story>

<Story name="Held Until Restart" asChild>
  <SwitchedOffAlerts rows={[offRow(true)]} {href} />
</Story>
