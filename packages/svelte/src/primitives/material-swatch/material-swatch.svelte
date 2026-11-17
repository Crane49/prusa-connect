<!--
  The colour of a filament.

  A dashed, unfilled box says that no colour is known, rather than a plausible
  grey a reader would take for a colour.

  Colour is never the only carrier (WCAG 1.4.1). The box's accessible name is
  the colour's own name, and the row beside it spells out the material.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import type { MaterialSwatchProps } from './types';

  let { material, class: className }: MaterialSwatchProps = $props();

  const BOX = 'block shrink-0 overflow-hidden rounded-sm border border-border';

  const colourName = $derived(
    material.colorName ?? t('primitives.materialSwatch.colour'),
  );
</script>

{#if material.colorRgba === undefined}
  <span
    role="img"
    data-slot="material-swatch"
    aria-label={t('primitives.materialSwatch.noColour')}
    title={t('primitives.materialSwatch.noColour')}
    class={[BOX, className, 'border-dashed bg-muted']}
  ></span>
{:else}
  <!-- A value from data, so it reaches the DOM through `style`. A computed
       class would not compile. -->
  <span
    role="img"
    data-slot="material-swatch"
    aria-label={colourName}
    title={colourName}
    class={[BOX, className]}
    style="background-color: {material.colorRgba};"
  ></span>
{/if}
