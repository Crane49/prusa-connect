<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { Device } from '../../domain';
  import { fakeDevice } from '../../test/fakes';
  import PrinterSwitcher from './printer-switcher.svelte';

  const { Story } = defineMeta({
    title: 'Shell/Printer Switcher',
    component: PrinterSwitcher,
  });

  const printer = (
    id: string,
    name: string,
    model: string,
    state: string,
  ): Device => ({
    ...fakeDevice(id, name),
    model,
    state: { '/state': state },
  });

  const printing = printer(
    'printer-1',
    'MK4S — Print Farm Rack 3, Shelf B',
    'MK4S',
    'PRINTING',
  );

  const attention = printer(
    'printer-2',
    'XL Multi-Tool — Print Lab, Bay 2',
    'XL',
    'ATTENTION',
  );
</script>

<Story name="Default" asChild>
  <PrinterSwitcher
    printers={[printing, attention]}
    chosenId={printing.id}
    onChoose={() => undefined}
  />
</Story>

<!-- Both machines simply working: no dot, no colour, two names and two badges.
     A mark that always shows says nothing. -->
<Story name="Nothing Wants A Person" asChild>
  <PrinterSwitcher
    printers={[
      printer('printer-1', 'MK4S — Rack 3', 'MK4S', 'PRINTING'),
      printer('printer-2', 'MK4S — Rack 4', 'MK4S', 'IDLE'),
    ]}
    chosenId="printer-2"
    onChoose={() => undefined}
  />
</Story>

<!-- Narrow: the badge has no room for its word, so each half carries a dot and
     states the same thing as `sr-only` text. -->
<Story name="Narrow" asChild>
  <div class="w-80">
    <PrinterSwitcher
      printers={[printing, attention]}
      chosenId={printing.id}
      onChoose={() => undefined}
    />
  </div>
</Story>
