<!--
  The slot component as the host mounts it, reading the session the setup
  holds. The story frame has no PrusaSlicer in it, so it draws nothing;
  SetAsCurrent's own stories show the action.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import type { ModuleHost } from '@prusa/connect-ui/slots';

  import { holdSession } from '../../session';
  import { farm, t } from '../../test/fakes';
  import SetAsCurrentRow from './set-as-current-row.svelte';

  holdSession({
    host: { t, signal: new AbortController().signal } as unknown as ModuleHost,
    api: {
      listSlicerPrinters: () => Promise.resolve({ printers: farm }),
    },
  });

  const { Story } = defineMeta({
    title: 'PrusaSlicer 3/SetAsCurrentRow',
    component: SetAsCurrentRow,
    args: { deviceId: 'mk4s-1' },
  });
</script>

<Story name="Outside PrusaSlicer" />
