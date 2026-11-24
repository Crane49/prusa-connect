// The service layer: this module's routes, typed from its own OpenAPI
// fragment. Every request goes through the host's client, so the core
// authorizes each one as the person on the page.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { bind, type components, type Operations } from 'buddy3d-camera-client';

export type Status = components['schemas']['Status'];
export type PairedCamera = components['schemas']['Camera'];
export type Paired = components['schemas']['Paired'];
export type Live = components['schemas']['Live'];
export type Direction = components['schemas']['CameraMove']['direction'];
export type Control = components['schemas']['CameraControl'];
export type Settings = components['schemas']['CameraSettings'];
export type Mode = NonNullable<Control['mode']>;
export type Quality = NonNullable<Control['video_quality']>;
export type Stream = NonNullable<Settings['stream']>;

/** The camera type this module offers, as its manifest names it. */
const CAMERA_TYPE = 'buddy3d';

export interface Buddy3dApi {
  readonly routes: Operations;
  /** The type a camera of this module is declared as: `<namespace>/buddy3d`. */
  readonly cameraType: string;
}

export const apiOn = (host: ModuleHost): Buddy3dApi => {
  // The host writes a module page's address as /m/<namespace>/<path>.
  const namespace = host.pageHref('cameras').split('/')[2] ?? '';

  return { routes: bind(host.client), cameraType: `${namespace}/${CAMERA_TYPE}` };
};

/**
 * The sentence this module's own refusal carries. The client reads only a
 * problem document, and the routes answer {code, message}, so it is read here.
 */
export const said = (error: unknown): string | undefined => {
  let body = (error as { body?: unknown } | undefined)?.body;
  if (typeof body === 'string') {
    try {
      body = JSON.parse(body);
    } catch {
      return undefined;
    }
  }
  const message = (body as { message?: unknown } | null | undefined)?.message;

  return typeof message === 'string' && message !== '' ? message : undefined;
};
