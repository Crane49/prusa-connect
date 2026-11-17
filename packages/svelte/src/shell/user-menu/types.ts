export interface UserMenuProps {
  principal?: string | undefined;
  /** A second line under the name in the menu, such as the person's address. */
  principalDetail?: string | undefined;
  onSignOut?: (() => void) | undefined;
}
