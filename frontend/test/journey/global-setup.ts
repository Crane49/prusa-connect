// One dev server for the lane, started before the browser opens.
import type { TestProject } from 'vitest/node';

import { startJourneyServer } from './server.ts';

declare module 'vitest' {
  interface ProvidedContext {
    journeyBaseUrl: string;
  }
}

export default async (project: TestProject): Promise<() => Promise<void>> => {
  const server = await startJourneyServer();
  project.provide('journeyBaseUrl', server.url);

  return async () => {
    await server.close();
  };
};
