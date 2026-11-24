<!--
  The section on a printer's Printer tab, in each state of its API key.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';
  import { fn } from 'storybook/test';

  import { t, TOKEN } from '../../test/fakes';
  import PrusalinkSection from './prusalink-section.svelte';

  const address =
    'http://192.168.1.20:8080/api/v1/modules/com.github.aux.prusa3d.prusaslicer2-connector/printers/01JF8K3M2Q/';

  const { Story } = defineMeta({
    title: 'PrusaSlicer 2 Connector/PrusalinkSection',
    component: PrusalinkSection,
    args: { t, address, onCreate: fn(), seen: { kind: 'never' } },
  });
</script>

<Story name="NoKey" args={{ key: { kind: 'none' } }} />
<Story name="Creating" args={{ key: { kind: 'creating' } }} />
<Story
  name="Created"
  args={{
    key: {
      kind: 'created',
      token: TOKEN,
    },
  }}
/>
<Story
  name="Failed"
  args={{
    key: {
      kind: 'failed',
      fault: {
        code: 'core.auth/ACCESS_DENIED',
        title: 'You may not do this.',
        detail: undefined,
      },
    },
  }}
/>
<Story
  name="SavingToStorage"
  args={{
    key: { kind: 'none' },
    keep: {
      keep: true,
      onKeepChange: fn(),
      storages: [
        { name: 'local', label: 'Local' },
        { name: 'nas', label: 'NAS' },
      ],
      storage: 'local',
      onStorageChange: fn(),
      folder: 'prints/mk4',
      onFolderChange: fn(),
      pickerAt: '',
      pickerFolders: [],
      onPickerWalk: fn(),
    },
  }}
/>
<Story
  name="SlicerSeen"
  args={{ key: { kind: 'none' }, seen: { kind: 'at', ago: '5 minutes ago' } }}
/>
<Story
  name="SlicerNeverSeen"
  args={{ key: { kind: 'none' }, seen: { kind: 'never' } }}
/>
<Story
  name="Phone"
  args={{ key: { kind: 'none' } }}
  globals={{ viewport: { value: 'phone' } }}
/>
