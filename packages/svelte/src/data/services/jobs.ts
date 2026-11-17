// The prints a printer has finished. A service names the SDK operation and its
// parameters and holds no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** One page of finished prints, as `list-device-jobs` answers it. */
export type JobHistoryPage = Awaited<
  ReturnType<PrusaConnect['listDeviceJobs']>
>;

/** One print, as the core records it. */
export type PrintJob = NonNullable<JobHistoryPage['items']>[number];

/** What reading a print history needs of the SDK, and no more. */
export type JobHistoryClient = Pick<PrusaConnect, 'listDeviceJobs'>;

/**
 * One page of the prints that have ended on a printer, newest first. An empty
 * cursor asks for the first page, and the answer's cursor asks for the next
 * one. The answer's cursor comes back empty on the last page.
 */
export const readJobHistory = (
  client: JobHistoryClient,
  deviceId: string,
  limit: number,
  cursor = '',
): Promise<JobHistoryPage> =>
  client.listDeviceJobs({
    path: { device_id: deviceId },
    query: cursor === '' ? { limit } : { cursor, limit },
  });

/** What reading one print by its id needs of the SDK, and no more. */
export type JobClient = Pick<PrusaConnect, 'getJob'>;

/** One print, by the id a link to it carries, wherever it ran. */
export const readJob = (client: JobClient, jobId: string): Promise<PrintJob> =>
  client.getJob({ path: { job_id: jobId } });
