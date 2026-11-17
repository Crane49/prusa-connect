<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { t } from '../../test/fakes';
  import DigestBadge from './digest-badge.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/DigestBadge',
    component: DigestBadge,
    args: { t },
  });
</script>

<Story name="Checked" args={{ digest: 'verified' }} />
<Story name="No checksum" args={{ digest: 'unverified' }} />
<Story name="Nothing to check" args={{ digest: 'none' }} />
