// One ended print as the core's jobs read answers it, ended `hoursAgo` before
// now.
export const settled = (
  deviceId: string,
  outcome: string,
  name = 'benchy.bgcode',
  hoursAgo = 1,
  seq = 1,
) => ({
  id: `${deviceId}-job-${String(seq)}`,
  device_job_id: String(seq),
  device_id: deviceId,
  path: `usb/${name}`,
  display_name: name,
  state: outcome === 'finished' ? 'FINISHED' : 'ERROR',
  progress: 100,
  time_remaining_ms: 0,
  started: new Date(Date.now() - (hoursAgo + 1) * 3_600_000).toISOString(),
  ended: new Date(Date.now() - hoursAgo * 3_600_000).toISOString(),
  outcome,
});
