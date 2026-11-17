<!--
  A searchable select facet inside its real popover, so a test can press
  Escape against the panel bits-ui portals to the body.

  This file holds no class of its own, so it adds nothing to what
  classes.test.ts compares.
-->
<script lang="ts">
  import FacetPopover from '../primitives/facet-popover/facet-popover.svelte';
  import SelectFacet from '../primitives/select-facet/select-facet.svelte';
</script>

<FacetPopover label="State" width={240}>
  <SelectFacet
    options={[{ value: 'IDLE', label: 'IDLE', count: 1 }]}
    selected={[]}
    onToggle={() => undefined}
    searchable
    searchAriaLabel="Search states"
  />
</FacetPopover>
