// The tile behind the glyph on an end screen, one static string per outcome
// (svelte-5.md). The design system draws the same tile on an alert dialog: a
// tinted square at the head of the message, never a full-width banner.
export const endTiles: Record<'success' | 'failure', string> = {
  success: 'bg-success text-success-foreground',
  failure: 'bg-destructive text-destructive-foreground',
};
