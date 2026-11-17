<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { DropdownMenuItem } from '../../primitives';
  import { SlotRegistry } from '../registry.svelte';
  import SlotActions from './slot-actions.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Slot Actions',
    component: SlotActions,
  });

  const registryWithActions = (): SlotRegistry => {
    const registry = new SlotRegistry();
    registry.declare('spools', {
      device_action: [
        { id: 'flush', title: 'Flush nozzle', destructive: false },
        { id: 'forget', title: 'Forget spool', destructive: true },
      ],
    });
    registry.registerAction('spools', 'device-action', 'flush', () =>
      Promise.resolve(),
    );
    registry.registerAction('spools', 'device-action', 'forget', () =>
      Promise.resolve(),
    );

    return registry;
  };
</script>

{#snippet own()}
  <DropdownMenuItem>Rename</DropdownMenuItem>
{/snippet}

<Story name="With Module Actions" asChild>
  <SlotActions
    name="device-action"
    deviceId="printer-1"
    label="CORE One"
    registry={registryWithActions()}
  />
</Story>

<Story name="With The Screen Items" asChild>
  <SlotActions
    name="device-action"
    deviceId="printer-1"
    label="CORE One"
    items={own}
    registry={registryWithActions()}
  />
</Story>

<!-- No manifest declares an action and the screen hands no items, so the
     menu is not drawn. -->
<Story name="With None" asChild>
  <SlotActions
    name="device-action"
    deviceId="printer-1"
    label="CORE One"
    registry={new SlotRegistry()}
  />
</Story>
