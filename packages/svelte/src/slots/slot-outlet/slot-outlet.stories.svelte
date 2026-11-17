<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import PlainSection from '../../test/plain-section.svelte';
  import ThrowingSection from '../../test/throwing-section.svelte';
  import { SlotRegistry } from '../registry.svelte';
  import SlotOutlet from './slot-outlet.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Slot Outlet',
    component: SlotOutlet,
  });

  const registryWith = (broken: boolean): SlotRegistry => {
    const registry = new SlotRegistry();
    registry.declare('spools', {
      device_section: [
        { id: 'spool-status', title: 'Loaded spool' },
        { id: 'broken', title: 'Broken' },
      ],
    });
    registry.registerSection(
      'spools',
      'device-section',
      'spool-status',
      PlainSection,
    );
    if (broken) {
      registry.registerSection(
        'spools',
        'device-section',
        'broken',
        ThrowingSection,
      );
    }

    return registry;
  };
</script>

<Story name="With A Contribution" asChild>
  <SlotOutlet
    screen="printer-detail"
    name="device-section"
    deviceId="printer-1"
    registry={registryWith(false)}
  />
</Story>

<Story name="With None" asChild>
  <SlotOutlet
    screen="printer-detail"
    name="device-section"
    deviceId="printer-1"
    registry={new SlotRegistry()}
  />
</Story>

<!-- The section that throws is dropped; the one beside it stays. -->
<Story name="One Section Throws" asChild>
  <SlotOutlet
    screen="printer-detail"
    name="device-section"
    deviceId="printer-1"
    registry={registryWith(true)}
  />
</Story>
