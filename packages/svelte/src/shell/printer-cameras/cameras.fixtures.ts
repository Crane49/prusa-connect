// The cameras the Cameras tab's stories and tests draw.
import type {
  Camera,
  CameraDeclaration,
  CameraDraft,
  DataFault,
} from '../../data';
import type {
  CameraPictures,
  CameraPrinterChoice,
  CameraSettingsSource,
} from './types';

export const PRINTER = 'pmk4s0000000000000000001';

export const PRINTER_NAME = 'MK4S Left';

export const PRINTERS: readonly CameraPrinterChoice[] = [
  { id: PRINTER, label: 'MK4S Left' },
  { id: 'pcore0000000000000000002', label: 'CORE One' },
];

export const declaration = (
  name: string,
  type: string,
  config: Record<string, unknown>,
): CameraDeclaration => ({
  name,
  type,
  config: { printer: PRINTER, ...config },
  version: '1',
});

export const CAMERAS: readonly CameraDeclaration[] = [
  declaration('bed-cam', 'push', {}),
  declaration('door-cam', 'mjpeg', {
    snapshot_url: 'http://192.168.1.40/snap.jpg',
  }),
  declaration('buddy3d', 'core/rtsp', {}),
  declaration('shelf-cam', 'core/generic', {
    stream_url: 'rtsp://192.168.1.41/live',
  }),
];

/** A store the card can draw, which records what it was asked. */
export const fakeSettings = (
  items: readonly CameraDeclaration[] = CAMERAS,
  state: {
    allowed?: boolean | undefined;
    loaded?: boolean;
    pending?: 'save' | 'remove' | undefined;
    error?: DataFault | undefined;
    failed?: 'save' | 'remove' | undefined;
  } = {},
): CameraSettingsSource & {
  readonly saved: CameraDraft[];
  readonly removed: string[];
} => {
  const saved: CameraDraft[] = [];
  const removed: string[] = [];

  return {
    saved,
    removed,
    items,
    types: [
      { type: 'generic' },
      {
        type: 'com.github.aux.acme.cams/buddy3d',
        title: 'Buddy3D camera',
        module: 'com.github.aux.acme.cams',
      },
    ],
    allowed: 'allowed' in state ? state.allowed : true,
    loaded: state.loaded ?? true,
    pending: state.pending,
    error: state.error,
    failed: state.failed,
    of: (printer: string) =>
      items.filter((camera) => camera.config.printer === printer),
    save: (draft: CameraDraft) => {
      saved.push(draft);

      return Promise.resolve(true);
    },
    remove: (camera: CameraDeclaration) => {
      removed.push(camera.name);

      return Promise.resolve(true);
    },
  };
};

export const pushUrlOf = (camera: string): string =>
  `/api/v1/cameras/${camera}/snapshot`;

export const NOT_WRITABLE: DataFault = {
  code: 'core.resource/RESOURCE_NOT_WRITABLE',
  status: 409,
  params: {},
  message: 'camera/door-cam is declared in the configuration file',
  traceId: undefined,
};

/**
 * The camera listing's row for each declaration: what the server answers for
 * the printer's page. A push camera takes stills and builds a stream from
 * them, an MJPEG camera here has a still address only, the generic camera
 * streams H.264 from an RTSP address, and the camera from the file emits
 * H.264 and is not running.
 */
export const listingOf = (
  items: readonly CameraDeclaration[] = CAMERAS,
): Camera[] =>
  items.map((item) => {
    const h264 =
      item.type === 'core/rtsp' ||
      (typeof item.config.stream_url === 'string' &&
        item.config.stream_url.startsWith('rtsp'));

    return {
      name: item.name,
      type: item.type,
      device_id: PRINTER,
      formats: h264 ? ['jpeg', 'h264/annexb'] : ['jpeg', 'mjpeg'],
      serving: item.type !== 'core/rtsp',
      snapshot: item.type !== 'core/rtsp',
      stream: item.type === 'push',
      watch: h264,
    };
  });

// A picture drawn in the page, so the story needs no camera.
const PICTURE =
  'data:image/svg+xml,' +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 9"><rect width="16" height="9" fill="#456"/><circle cx="8" cy="4.5" r="2" fill="#fa6"/></svg>',
  );

export const picturesOf = (row: Camera): CameraPictures => ({
  still: row.snapshot ? `${PICTURE}#still-${row.name}` : undefined,
  live: row.stream ? `${PICTURE}#live-${row.name}` : undefined,
});
