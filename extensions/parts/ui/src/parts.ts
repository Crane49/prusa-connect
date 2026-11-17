// The service layer: the module's own routes, reached through the host's
// client (svelte-data.md, frontend-structure.md).
//
// `bind` comes from `parts-client`, the typed client generated from
// `extensions/parts/openapi.json`. It attaches one method per operation onto the
// transport of a `@prusa/connect` client, so a call carries that
// client's credential and goes to the rebased path under the module's mount.
//
// The client the host hands over is in session mode, so the credential is the
// cookie the browser already holds. Nothing here names a URL, a header or a
// token.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { bind, type components, type operations } from 'parts-client';

/** One folder of the tree, with how much it holds directly. */
export type Folder = components['schemas']['Folder'];

/** What a caller sends to make or to change a folder. */
export type FolderDraft = components['schemas']['FolderDraft'];

/** One part as a listing shows it. */
export type PartRow = components['schemas']['PartRow'];

/** One part with its versions and comments. */
export type PartDetail = components['schemas']['PartDetail'];

/** What a caller sends to make or to change a part. */
export type PartDraft = components['schemas']['PartDraft'];

/** One version of a part. */
export type Version = components['schemas']['Version'];

/** One file of a version. */
export type PartFile = components['schemas']['File'];

/** One file a new version is to hold. */
export type FileDraft = components['schemas']['FileDraft'];

/** One comment on a part. */
export type Comment = components['schemas']['Comment'];

/** The file a listing draws for a part. */
export type Picture = components['schemas']['Picture'];

/** One tag and how many parts carry it. */
export type Tag = components['schemas']['Tag'];

/** What a delete leaves for the page to clean up in the storage. */
export type Removed = components['schemas']['Removed'];

/** The catalogue as one screen frames it. */
export type Overview =
  operations['read-overview']['responses'][200]['content']['application/json'];

/** One page of a listing. */
export type Listing =
  operations['list-parts']['responses'][200]['content']['application/json'];

/** What a listing is narrowed to. A field left out keeps every part. */
export type PartQuery = NonNullable<
  operations['list-parts']['parameters']['query']
>;

/** What the stores need of the module's routes. A test hands in a fake. */
export interface PartsService {
  overview(signal?: AbortSignal): Promise<Overview>;
  list(query: PartQuery, signal?: AbortSignal): Promise<Listing>;
  part(id: number, signal?: AbortSignal): Promise<PartDetail>;
  addFolder(draft: FolderDraft): Promise<Folder>;
  changeFolder(id: number, draft: FolderDraft): Promise<Folder>;
  deleteFolder(id: number): Promise<void>;
  addPart(draft: PartDraft): Promise<PartDetail>;
  changePart(id: number, draft: PartDraft): Promise<PartDetail>;
  deletePart(id: number): Promise<Removed>;
  addVersion(id: number, note: string, files: FileDraft[]): Promise<PartDetail>;
  changeVersion(id: number, number: number, note: string): Promise<PartDetail>;
  deleteVersion(
    id: number,
    number: number,
  ): Promise<{ removed: Removed; part: PartDetail }>;
  addComment(id: number, body: string): Promise<Comment>;
  changeComment(id: number, comment: number, body: string): Promise<Comment>;
  deleteComment(id: number, comment: number): Promise<void>;
}

/** The module's routes, bound onto the client the host handed this module. */
export const partsOn = (host: ModuleHost): PartsService => {
  const routes = bind(host.client);
  // Every call also ends with the host's signal, so nothing this module asked
  // for carries on once the person who admitted it has left.
  const ended = (signal?: AbortSignal): AbortSignal =>
    signal === undefined ? host.signal : AbortSignal.any([signal, host.signal]);

  return {
    overview: async (signal) => await routes.readOverview({}, ended(signal)),
    list: async (query, signal) =>
      await routes.listParts({ query }, ended(signal)),
    part: async (id, signal) =>
      (await routes.getPart({ path: { id } }, ended(signal))).part,
    addFolder: async (draft) =>
      (await routes.createFolder({ body: draft }, ended())).folder,
    changeFolder: async (id, draft) =>
      (await routes.changeFolder({ path: { id }, body: draft }, ended()))
        .folder,
    deleteFolder: async (id) => {
      await routes.deleteFolder({ path: { id } }, ended());
    },
    addPart: async (draft) =>
      (await routes.createPart({ body: draft }, ended())).part,
    changePart: async (id, draft) =>
      (await routes.changePart({ path: { id }, body: draft }, ended())).part,
    deletePart: async (id) =>
      (await routes.deletePart({ path: { id } }, ended())).removed,
    addVersion: async (id, note, files) =>
      (
        await routes.createVersion(
          { path: { id }, body: { note, files } },
          ended(),
        )
      ).part,
    changeVersion: async (id, number, note) =>
      (
        await routes.changeVersion(
          { path: { id, number }, body: { note } },
          ended(),
        )
      ).part,
    deleteVersion: async (id, number) => {
      const answer = await routes.deleteVersion(
        { path: { id, number } },
        ended(),
      );

      return { removed: answer.removed, part: answer.part };
    },
    addComment: async (id, body) =>
      (await routes.createComment({ path: { id }, body: { body } }, ended()))
        .comment,
    changeComment: async (id, comment, body) =>
      (
        await routes.changeComment(
          { path: { id, comment }, body: { body } },
          ended(),
        )
      ).comment,
    deleteComment: async (id, comment) => {
      await routes.deleteComment({ path: { id, comment } }, ended());
    },
  };
};
