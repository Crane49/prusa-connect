<!-- A view toggle that holds its own choice, for the view-mode tests. The
     component is controlled, so a toggle with nothing behind it cannot move. -->
<script lang="ts">
  import { untrack } from 'svelte';

  import type { ViewModeName } from '../primitives/view-mode';
  import { ViewModeToggle, viewModes } from '../primitives/view-mode';

  let {
    initial = 'table',
    modes = ['table', 'rows'],
    unavailable,
  }: {
    initial?: string;
    modes?: ViewModeName[];
    /** The one view that needs more room than the listing has, by name. */
    unavailable?: ViewModeName;
  } = $props();

  // The probe seeds itself once. `untrack` says so, where a bare read warns.
  let value = $state(untrack(() => initial));

  const options = $derived(
    viewModes(
      modes,
      unavailable ? { [unavailable]: 'It needs a wider window.' } : undefined,
    ),
  );
</script>

<ViewModeToggle
  {value}
  {options}
  onValueChange={(next: string) => (value = next)}
/>
<p data-testid="chosen">{value}</p>
