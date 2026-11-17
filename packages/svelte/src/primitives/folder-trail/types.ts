/** One step of the path, as a reader sees it and as the listing addresses it. */
export interface FolderCrumb {
  label: string;
  path: string;
}

export interface FolderTrailProps {
  /** Outermost first. The last one is where the reader is. */
  crumbs: readonly FolderCrumb[];
  label: string;
  onOpen: (path: string) => void;
}
