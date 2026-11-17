// What a dialog of the screen is open for. Each dialog is closed while its act
// is undefined, and a new act makes its form new, so nothing typed into an
// abandoned dialog comes back.

/** A new dashboard, or a new name for the one being changed. */
export type DashboardAct =
  | { readonly kind: 'create' }
  | {
      readonly kind: 'rename';
      readonly name: string;
    };
