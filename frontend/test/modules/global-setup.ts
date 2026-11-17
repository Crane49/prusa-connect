// One dev server for the lane, in front of the core PCL_CORE_URL names.
import type { TestProject } from 'vitest/node';

import { startAppServer } from './server.ts';

declare module 'vitest' {
  interface ProvidedContext {
    moduleAppUrl: string;
  }
}

export default async (project: TestProject): Promise<() => Promise<void>> => {
  const core = process.env.PCL_CORE_URL;
  if (core === undefined || core === '') {
    throw new Error(
      'PCL_CORE_URL names the core this lane drives. scripts/check-example-spools.sh sets it.',
    );
  }

  const server = await startAppServer(core);
  project.provide('moduleAppUrl', server.url);

  return async () => {
    await server.close();
  };
};
