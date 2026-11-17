// The service layer: the three routes of this module, through the typed
// client generated from its fragment (frontend-structure.md). It holds no
// state and knows no framework, so a test hands it a stub host.
import type { PrusaConnect } from '@prusa/connect';
import { bind, type components } from 'threemf-client';

/**
 * What one 3MF project holds, as get-project answers: the slicer that wrote
 * it, the printer setups with their beds, the objects with every copy on the
 * plate, the pictures and what could not be read.
 */
export type ProjectDocument = components['schemas']['Project'];

/**
 * One object of a 3MF project as triangles, as get-project-mesh answers:
 * flat vertices in millimetres, the triangles as indices into them, and the
 * model parts in the order their triangles come.
 */
export type ProjectMesh = components['schemas']['ProjectMesh'];

/** What the store needs of the module's routes. A test hands in a fake. */
export interface ProjectService {
  /** What one 3MF project holds. */
  project(
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<ProjectDocument>;
  /** One object of a 3MF project as triangles. */
  mesh(
    storage: string,
    path: string,
    object: number,
    signal?: AbortSignal,
  ): Promise<ProjectMesh>;
  /** The address of one picture of a project, for an `img`. */
  thumbnailUrl(storage: string, path: string, index: number): string;
}

/** The mount the host puts this module's routes under. */
const MOUNT = '/api/v1/modules/com.github.aux.prusa3d.threemf';

/** The service over the client the host handed this module. */
export const projectsOn = (client: PrusaConnect): ProjectService => {
  const routes = bind(client);

  return {
    project: (storage, path, signal) =>
      routes.getProject({ query: { storage, path } }, signal),
    mesh: (storage, path, object, signal) =>
      routes.getProjectMesh({ query: { storage, path, object } }, signal),
    // An `img` sends the session cookie, which is what the host's door reads
    // for a page it serves.
    thumbnailUrl: (storage, path, index) =>
      `${client.url}${MOUNT}/project/thumbnail` +
      `?storage=${encodeURIComponent(storage)}` +
      `&path=${encodeURIComponent(path)}&index=${String(index)}`,
  };
};
