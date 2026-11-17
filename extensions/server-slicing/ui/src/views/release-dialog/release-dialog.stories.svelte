<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { ReleaseActs } from '../../release-acts.svelte';
  import { adminSlicers, t, viewOf } from '../../test/fakes';
  import ReleaseDialog from './release-dialog.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ReleaseDialog',
    component: ReleaseDialog,
    args: { t },
  });

  const asking = (kind: 'unverified' | 'remove', version: string) => {
    const acts = new ReleaseActs(viewOf('ready', adminSlicers));
    acts.asking = { kind, version };
    return acts;
  };
</script>

<Story
  name="Install without a checksum"
  args={{ acts: asking('unverified', '2.9.2') }}
/>
<Story name="Remove" args={{ acts: asking('remove', '2.9.6') }} />
