// What the API tab reads out of the module's own OpenAPI fragment: the same
// file the Go half embeds and the host serves at <mount>/openapi.json. The
// routes themselves are drawn by the core's reference page, framed in the tab
// (ADR-20261027-zyf). This file answers the two things the getting-started
// part above it needs: which scope each route takes, and why a job fails.
// Plain functions, so a test reads them without a browser.
import fragment from '../../openapi.json';

/** Who may call a route: the scope a token needs. */
export type Scope = 'read' | 'operate' | 'administer';

export interface Operation {
  id: string;
  method: string;
  /** Relative to the module's mount, such as /jobs/{id}. */
  path: string;
  scope: Scope;
}

interface Fragment {
  paths: Record<
    string,
    Record<string, { operationId: string; 'x-scope'?: string }>
  >;
  components: {
    schemas: Record<
      string,
      { properties?: Record<string, { enum?: readonly unknown[] }> }
    >;
  };
}

const document = fragment as unknown as Fragment;

const ORDER = ['get', 'post', 'put', 'delete'];

/** Every operation the fragment describes, in the order of its paths. */
export const operations = (): Operation[] =>
  Object.entries(document.paths).flatMap(([path, methods]) =>
    Object.entries(methods)
      .sort(([a], [b]) => ORDER.indexOf(a) - ORDER.indexOf(b))
      .map(([method, op]): Operation => ({
        id: op.operationId,
        method: method.toUpperCase(),
        path,
        scope: (op['x-scope'] ?? 'read') as Scope,
      })),
  );

/** The codes a job fails with, which a job's error carries rather than an answer. */
export const jobFailures = (): string[] =>
  (document.components.schemas.JobError?.properties?.code?.enum ?? []).map(
    String,
  );
