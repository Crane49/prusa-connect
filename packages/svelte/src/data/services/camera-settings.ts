// The cameras an operator adds from the app, read and written through the
// camera API (ADR-20261116-g5x). A service names the SDK operation and its
// parameters and keeps no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the camera settings store needs of the SDK, and no more. */
export type CameraSettingsClient = Pick<
  PrusaConnect,
  | 'listCameraSettings'
  | 'putCamera'
  | 'deleteCamera'
  | 'checkAccess'
  | 'listCameraTypes'
>;

/**
 * One type a new camera may be declared as: the core's own, which names no
 * module, or one an installed module offers, with the title it gave it.
 */
export type CameraTypeChoice = NonNullable<
  Awaited<ReturnType<PrusaConnect['listCameraTypes']>>['items']
>[number];

/** The types a new camera may be declared as. */
export const readCameraTypes = async (
  client: CameraSettingsClient,
): Promise<readonly CameraTypeChoice[]> => {
  const page = await client.listCameraTypes();

  return page.items ?? [];
};

type SettingsPage = Awaited<ReturnType<PrusaConnect['listCameraSettings']>>;

/** One camera's settings, as the listing answers them. */
export type CameraDeclaration = NonNullable<SettingsPage['items']>[number];

/** The config key a camera names its printer under, which the host reads. */
export const PRINTER_KEY = 'printer';

/** Every camera. */
export const readCameraDeclarations = async (
  client: CameraSettingsClient,
): Promise<readonly CameraDeclaration[]> => {
  const page = await client.listCameraSettings();

  return page.items ?? [];
};

/**
 * Writes one camera. `version` is the version last read, and without it the
 * write only creates, so a name already taken is refused rather than replaced.
 */
export const writeCamera = (
  client: CameraSettingsClient,
  name: string,
  type: string,
  config: Readonly<Record<string, unknown>>,
  version?: string,
): Promise<unknown> =>
  client.putCamera({
    path: { camera: name },
    header:
      version === undefined
        ? { 'If-None-Match': '*' }
        : { 'If-Match': `"${version}"` },
    body: { type, config: { ...config } },
  });

/** Removes one camera, fenced on the version last read. */
export const removeCamera = (
  client: CameraSettingsClient,
  name: string,
  version: string,
): Promise<unknown> =>
  client.deleteCamera({
    path: { camera: name },
    header: { 'If-Match': `"${version}"` },
  });

/** Whether the person may declare cameras, which the server decides per kind. */
export const mayConfigureCameras = async (
  client: CameraSettingsClient,
): Promise<boolean> => {
  const decision = await client.checkAccess({
    body: {
      action: 'configure_resource',
      object_type: 'resource_kind',
      object_id: 'camera',
    },
  });

  return decision.allowed;
};
