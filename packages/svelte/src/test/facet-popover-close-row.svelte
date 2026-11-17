<!--
  One row inside a facet's panel that shuts the panel when it is pressed, the
  way a single-choice list does.

  This file holds no class of its own, so it adds nothing to what
  classes.test.ts compares.
-->
<script lang="ts">
  import { facetPopoverClose } from '../primitives/facet-popover/context';

  const close = facetPopoverClose();
</script>

<button type="button" onclick={close}>Ready</button>
