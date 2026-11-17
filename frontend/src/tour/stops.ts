// What the tour pages share: the address of each stop and the answers that
// draw the two stops before sign-in.
import { json, problem } from '../test/stub';

export const PRINTER = '/printers/34e039fc-13fd-4f4a-b16b-92392643f63a/status';

export const signedOut = {
  'GET /api/v1/principal': () => problem(401, 'unauthenticated'),
};

export const notSetUp = {
  'GET /api/v1/setup': () =>
    json(200, {
      state: 'pending',
      version: '0.0.0-dev',
      issuer: false,
      modules: [],
      steps: [],
    }),
};
