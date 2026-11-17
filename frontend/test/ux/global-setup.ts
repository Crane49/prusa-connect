// One server for the whole lane. Building and starting one per file would pay
// for the build every time.
import type { TestProject } from 'vitest/node';

import { startUxServer } from './server.ts';

declare module 'vitest' {
  interface ProvidedContext {
    uxBaseUrl: string;
  }
}

export default async (project: TestProject): Promise<() => Promise<void>> => {
  const server = await startUxServer();
  project.provide('uxBaseUrl', server.url);

  return async () => {
    await server.close();
  };
};
