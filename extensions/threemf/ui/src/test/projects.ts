// A store over a fake service, for the stories and the tests of the views
// that read the store. The fake answers by the file's path, so stories drawn
// together on one page each read their own file.
import type { ProjectDocument, ProjectMesh, ProjectService } from '../api';
import { Projects } from '../projects.svelte';
import { holdProjects } from '../session';
import { twoPartMesh } from './project-meshes';
import { multibed, plateThumbnail } from './project-fixtures';

/**
 * A refusal in the shape the client throws one: a code, a status and the
 * server's English line. The store reads it by shape (`faultOf`).
 */
export interface Refusal {
  readonly code: string;
  readonly status: number;
  readonly message: string;
}

export const refusal = (status: number, code: string): Refusal => ({
  code,
  status,
  message: `${code}: refused in a test`,
});

/** What the fake answers for one path. */
export type Answer =
  | { readonly project: ProjectDocument }
  | { readonly refused: Refusal }
  | { readonly pending: true };

/** The paths every story and test may read. */
export const ANSWERS: Readonly<Record<string, Answer>> = {
  'projects/multibed.3mf': { project: multibed },
  'projects/loading.3mf': { pending: true },
  'projects/damaged.3mf': {
    refused: refusal(422, 'threemf/NOT_A_PROJECT'),
  },
  'projects/hidden.3mf': { refused: refusal(404, 'threemf/NO_FILE') },
};

const never = new Promise<never>(() => undefined);

/** A service that answers from a table, and records every read it was asked. */
export const fakeService = (
  answers: Readonly<Record<string, Answer>> = ANSWERS,
  mesh: ProjectMesh = twoPartMesh,
): ProjectService & { readonly asked: string[] } => {
  const asked: string[] = [];

  return {
    asked,
    project: (storage, path) => {
      asked.push(`${storage}:${path}`);
      const answer = answers[path];
      if (answer === undefined)
        // eslint-disable-next-line @typescript-eslint/prefer-promise-reject-errors -- the shape the client throws
        return Promise.reject(refusal(404, 'threemf/NO_FILE'));
      if ('pending' in answer) return never;
      // eslint-disable-next-line @typescript-eslint/prefer-promise-reject-errors -- the shape the client throws
      if ('refused' in answer) return Promise.reject(answer.refused);

      return Promise.resolve(answer.project);
    },
    mesh: () => Promise.resolve(mesh),
    thumbnailUrl: (_storage, _path, index) => plateThumbnail(index),
  };
};

/** Holds a store over the fake for the views to read, and answers it. */
export const holdFakeProjects = (
  answers?: Readonly<Record<string, Answer>>,
): Projects => {
  const projects = new Projects(fakeService(answers));
  holdProjects(projects, new AbortController().signal);

  return projects;
};
