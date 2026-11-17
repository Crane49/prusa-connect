// Static strings chosen by key (svelte-5.md). Tailwind reads this file, so a
// utility written here is emitted; one assembled at run time is not.

/** What the plan's legend names: what a copy's colour does not say. */
export type LegendEntry =
  'ticked' | 'nonPrintable' | 'partlyOutside' | 'outside';

// The legend's swatch for each entry, drawn the way the plan draws the copy:
// a ticked copy in the brand outline, a copy left out of the print grey and
// dashed, and a copy off its bed outlined in the colour of that placement.
export const legendSwatch: Readonly<Record<LegendEntry, string>> = {
  ticked: 'border-2 border-primary bg-muted-foreground/40',
  nonPrintable: 'border-dashed border-muted-foreground bg-muted',
  partlyOutside: 'border-2 border-warning-text bg-muted-foreground/40',
  outside: 'border-2 border-destructive bg-muted-foreground/40',
};
