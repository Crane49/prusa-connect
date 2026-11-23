<!--
  Every state the print dialog draws. Omitted Refused for one printer: the
  module answers a person only the printers they may print on, so a refused
  read is the whole list, which the Refused story shows.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { farm, mk4sRequest, pickerOf, t } from '../../test/fakes';
  import SelectPrinterPage from './select-printer-page.svelte';

  const { Story } = defineMeta({
    title: 'PrusaSlicer 3/SelectPrinterPage',
    component: SelectPrinterPage,
    args: { t },
  });
</script>

<Story name="Loading" args={{ view: pickerOf({ load: 'loading' }) }} />
<Story name="Failed" args={{ view: pickerOf({ load: 'failed' }) }} />
<Story name="Refused" args={{ view: pickerOf({ load: 'refused' }) }} />
<Story
  name="Empty"
  args={{
    view: pickerOf({ request: { printerModel: 'MINI' }, printers: farm }),
  }}
/>
<Story
  name="Waiting for the slicer"
  args={{ view: pickerOf({ request: undefined }) }}
/>
<Story name="Printers" args={{ view: pickerOf() }} />
<Story name="No file name" args={{ view: pickerOf({ filename: '' }) }} />
<Story
  name="Several beds"
  args={{ view: pickerOf({ request: { ...mk4sRequest, multipleBeds: true } }) }}
/>
<Story name="Sent" args={{ view: pickerOf({ sent: true }) }} />
<Story
  name="Outside PrusaSlicer"
  args={{ view: pickerOf({ inSlicer: false }) }}
/>
