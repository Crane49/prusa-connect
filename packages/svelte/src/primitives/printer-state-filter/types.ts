import type { ControlSize } from '../control-trigger/types';

/** One dimension inside the facet: what it lists, what is picked, how to read it. */
export interface PrinterFacetDimension {
  /**
   * The values this fleet actually contains, in its own order. A list of
   * values nobody owns is a list of zeroes.
   */
  values: string[];
  /** What the caller's facet counter returned for this dimension. A miss reads as 0. */
  counts: Map<string, number>;
  selected: string[];
  onSelectedChange: (next: string[]) => void;
  /** How each value should read to a person. A raw enum is not a label. */
  labelFor?: ((value: string) => string) | undefined;
}

export interface PrinterStateFilterProps {
  /** Matches the row it stands in, on the shared control scale. */
  size?: ControlSize | undefined;
  /** The coarse tier. Omit it to filter on raw state alone. */
  severity?: PrinterFacetDimension | undefined;
  state: PrinterFacetDimension;
  label?: string | undefined;
  width?: number | undefined;
}
