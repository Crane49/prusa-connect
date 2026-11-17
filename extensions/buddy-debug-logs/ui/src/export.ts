// A download, put together in the browser. One answer through the mount is
// at most 1 MiB, so the module serves a stream as pages and this asks for
// each in turn and joins them into one file. A purge while it runs moves the
// records, and the module refuses the next cursor; the download then starts
// again once from the first page, so the file holds no gap and no repeat.
import type { LogsApi } from './api';
import { isStale } from './faults';

export type Stream = 'syslog' | 'metrics';

/** The most pages one download asks for: 512 KiB each, 64 MiB in all. */
export const MAX_PAGES = 128;

/** The file a stream is saved as. */
export const fileName = (stream: Stream, at: number): string =>
  `printer-${stream}-${new Date(at).toISOString().replaceAll(':', '-').slice(0, 19)}.${
    stream === 'syslog' ? 'log' : 'lp'
  }`;

/** Every page of one stream, joined, as the file a person saves. */
export const exportStream = async (
  api: Pick<LogsApi, 'exportRecords'>,
  query: { stream: Stream; device?: string; since?: string },
  signal?: AbortSignal,
): Promise<Blob> => {
  try {
    return await pagesOf(api, query, signal);
  } catch (thrown) {
    if (!isStale(thrown)) throw thrown;

    return pagesOf(api, query, signal);
  }
};

const pagesOf = async (
  api: Pick<LogsApi, 'exportRecords'>,
  query: { stream: Stream; device?: string; since?: string },
  signal?: AbortSignal,
): Promise<Blob> => {
  const parts: string[] = [];
  let cursor: string | undefined;

  for (let page = 0; page < MAX_PAGES; page += 1) {
    const answer = await api.exportRecords(
      { query: { ...query, ...(cursor === undefined ? {} : { cursor }) } },
      signal,
    );
    parts.push(answer.text);
    cursor = answer.next;
    if (cursor === undefined || cursor === '') break;
  }

  return new Blob(parts, { type: 'text/plain;charset=utf-8' });
};
