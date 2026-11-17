// The store of the projects this page read: one document per file, and the
// meshes of the objects a person opened. A shared store, so a class
// (svelte-5.md). The service comes in through the constructor, which lets a
// test hand in a fake.
//
// One store serves the file view and every other module that draws the
// exported project view, so two views of one file cost one read.
import { type DataFault, faultOf } from '@prusa/connect-ui/data';

import type { ProjectDocument, ProjectMesh, ProjectService } from './api';

/**
 * What one 3MF project turned out to hold, or why the module could not read
 * it. A project the module refused is told apart from one not asked about
 * yet, which `described` answers as undefined.
 */
export type ProjectDescription =
  | { readonly project: ProjectDocument; readonly fault?: undefined }
  | { readonly project?: undefined; readonly fault: DataFault };

/**
 * One object of a 3MF project as triangles, or why the module could not
 * read it. Undefined from `describedMesh` until the read lands.
 */
export type MeshDescription =
  | { readonly mesh: ProjectMesh; readonly fault?: undefined }
  | { readonly mesh?: undefined; readonly fault: DataFault };

const keyOf = (storage: string, path: string): string => `${storage}:${path}`;

/** The key of one object of one project. A path holds no NUL. */
const meshKeyOf = (storage: string, path: string, object: number): string =>
  `${keyOf(storage, path)}\u0000${String(object)}`;

export class Projects {
  readonly #service: ProjectService;
  // The server owns these, so they are replaced whole (svelte-5.md).
  #projects = $state.raw<Readonly<Record<string, ProjectDescription>>>({});
  #meshes = $state.raw<Readonly<Record<string, MeshDescription>>>({});
  // The reads in flight, by key. Nothing draws them, so they are not
  // reactive.
  readonly #reading: Record<string, Promise<void> | undefined> = {};
  readonly #meshing: Record<string, Promise<void> | undefined> = {};

  constructor(service: ProjectService) {
    this.#service = service;
  }

  /** What one project held, or undefined until `describe` has answered. */
  described(storage: string, path: string): ProjectDescription | undefined {
    return this.#projects[keyOf(storage, path)];
  }

  /**
   * Asks the module what one 3MF project holds. A project already described
   * costs nothing, and a call for one being read waits for that read. A
   * refusal is kept with its code, so a page can say why it shows no project.
   * A read that was stopped records nothing.
   */
  async describe(
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<void> {
    const key = keyOf(storage, path);

    for (;;) {
      if (key in this.#projects || signal?.aborted === true) return;
      const running = this.#reading[key];
      if (running === undefined) break;
      await running;
    }

    const read = this.#read(key, storage, path, signal);
    this.#reading[key] = read;
    try {
      await read;
    } finally {
      if (this.#reading[key] === read) this.#reading[key] = undefined;
    }
  }

  async #read(
    key: string,
    storage: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<void> {
    try {
      const project = await this.#service.project(storage, path, signal);
      this.#projects = { ...this.#projects, [key]: { project } };
    } catch (thrown) {
      if (signal?.aborted !== true) {
        this.#projects = {
          ...this.#projects,
          [key]: { fault: faultOf(thrown) },
        };
      }
    }
  }

  /**
   * One object of a project as triangles, once the module has answered.
   * Undefined while it has not, which a page tells apart from a refusal.
   */
  describedMesh(
    storage: string,
    path: string,
    object: number,
  ): MeshDescription | undefined {
    return this.#meshes[meshKeyOf(storage, path, object)];
  }

  /**
   * Asks the module for one object of a project as triangles. An object
   * already described costs nothing, and a call for one being read waits for
   * that read. A refusal is kept with its code.
   */
  async describeMesh(
    storage: string,
    path: string,
    object: number,
    signal?: AbortSignal,
  ): Promise<void> {
    const key = meshKeyOf(storage, path, object);
    if (key in this.#meshes || signal?.aborted === true) return;
    const running = this.#meshing[key];
    if (running !== undefined) {
      await running;

      return;
    }

    const read = (async () => {
      try {
        const mesh = await this.#service.mesh(storage, path, object, signal);
        this.#meshes = { ...this.#meshes, [key]: { mesh } };
      } catch (thrown) {
        if (signal?.aborted !== true) {
          this.#meshes = { ...this.#meshes, [key]: { fault: faultOf(thrown) } };
        }
      }
    })();
    this.#meshing[key] = read;
    try {
      await read;
    } finally {
      if (this.#meshing[key] === read) this.#meshing[key] = undefined;
    }
  }

  /** Where one picture of a project is served from, by its index. */
  thumbnailUrl(storage: string, path: string, index: number): string {
    return this.#service.thumbnailUrl(storage, path, index);
  }
}
