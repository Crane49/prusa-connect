import type { Snippet } from 'svelte';

import type { BedSelection, ProjectSelectionActs } from '../project-view';

/**
 * What the project view takes, as this module hands it to other modules
 * under the name `project-view` (ui.exports in module.yaml). The props
 * follow this module's version as its routes do (ADR-20261027-wvj rule 19).
 *
 * A module that draws it names this module under `require` or `suggest`
 * and the component under `ui.imports`, then loads it:
 *
 *     const view = await host.components.load(
 *       'com.github.aux.prusa3d.threemf',
 *       'project-view',
 *     );
 *
 * and draws it with these props.
 */
export interface ProjectFileProps {
  /** The storage the file is in, by the name it is declared under. */
  storage: string;
  /** The 3MF file, relative to the storage root. */
  path: string;
  /**
   * The page's buttons for the ticked copies, drawn in the selection bar,
   * such as one that prints or slices them.
   */
  selectionActs?: Snippet<[ProjectSelectionActs]> | undefined;
  /**
   * Told the ticked copies, one selection per bed, each time a person
   * changes them. A selection names the bed and the build items, the shape
   * the threemf Go package takes a selection in.
   */
  onselect?: ((selections: readonly BedSelection[]) => void) | undefined;
  /**
   * Whether the overview draws the project's picture. True by default. The
   * file view passes false, because the file's page shows it in its header.
   */
  picture?: boolean | undefined;
}
