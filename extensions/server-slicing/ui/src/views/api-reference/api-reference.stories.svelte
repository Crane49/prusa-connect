<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { t } from '../../test/fakes';
  import ApiReference from './api-reference.svelte';

  const { Story } = defineMeta({
    title: 'Server slicing/ApiReference',
    component: ApiReference,
    args: {
      t,
      apiBase:
        'http://printers.local:8080/api/v1/modules/com.github.aux.prusa3d.server-slicing',
      coreBase: 'http://printers.local:8080/api/v1',
      tokensHref: '/settings/tokens',
      referenceHref: '/docs?module=com.github.aux.prusa3d.server-slicing',
    },
  });
</script>

<Story name="Reference" />
<Story
  name="Phone"
  globals={{ viewport: { value: 'phone', isRotated: false } }}
/>
<Story name="Dark" globals={{ theme: 'dark' }} />
