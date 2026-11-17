// A small WebGL viewer for one object of a 3MF project: the triangles the
// mesh route answers, each part in the colour of its filament, lit from two
// sides, turned by dragging and brought nearer by the wheel or a pinch.
//
// It is loaded on demand by the object panel, so its code stays out of the
// page until a person opens an object. It draws only when something changed
// and never runs a loop of its own.

/** What the viewer draws. */
export interface ViewerMesh {
  /** x, y and z in millimetres, z up. */
  readonly vertices: readonly number[];
  /** Three vertex indices a triangle. */
  readonly triangles: readonly number[];
  /** How many triangles each part has, in order, with its colour. */
  readonly parts: readonly {
    readonly triangles: number;
    readonly colour: Rgb;
  }[];
}

/** A colour as three numbers from 0 to 1. */
export type Rgb = readonly [number, number, number];

/** What the page can do to a viewer it made. */
export interface Viewer {
  /** Turns the object by the given angles in radians. */
  turn(yaw: number, pitch: number): void;
  /** Brings the object nearer for a factor above 1, further below it. */
  zoom(factor: number): void;
  /** Back to the view it opened with. */
  reset(): void;
  /** Draws again after the canvas changed size. */
  resize(): void;
  /** Lets go of the WebGL context and every listener. */
  destroy(): void;
}

const VERTEX = `
attribute vec3 position;
attribute vec3 normal;
attribute vec3 colour;
uniform mat4 view;
varying vec3 vNormal;
varying vec3 vColour;
void main() {
  vNormal = normal;
  vColour = colour;
  gl_Position = view * vec4(position, 1.0);
}`;

// Two lights and an ambient term. The absolute value of the light lights both
// sides of a triangle, because a file's triangles do not all face out.
const FRAGMENT = `
precision mediump float;
varying vec3 vNormal;
varying vec3 vColour;
void main() {
  vec3 n = normalize(vNormal);
  float key = abs(dot(n, normalize(vec3(0.4, -0.6, 0.7))));
  float fill = abs(dot(n, normalize(vec3(-0.6, 0.5, 0.2))));
  float light = 0.35 + 0.55 * key + 0.2 * fill;
  gl_FragColor = vec4(vColour * light, 1.0);
}`;

type Mat4 = Float32Array;

const multiply = (a: Mat4, b: Mat4): Mat4 => {
  const out = new Float32Array(16);
  for (let col = 0; col < 4; col += 1) {
    for (let row = 0; row < 4; row += 1) {
      let sum = 0;
      for (let k = 0; k < 4; k += 1) {
        sum += (a[k * 4 + row] ?? 0) * (b[col * 4 + k] ?? 0);
      }
      out[col * 4 + row] = sum;
    }
  }

  return out;
};

const perspective = (
  fov: number,
  aspect: number,
  near: number,
  far: number,
) => {
  const f = 1 / Math.tan(fov / 2);
  const out = new Float32Array(16);
  out[0] = f / aspect;
  out[5] = f;
  out[10] = (far + near) / (near - far);
  out[11] = -1;
  out[14] = (2 * far * near) / (near - far);

  return out;
};

// A camera on a sphere around the centre, z up, looking at the centre.
const orbit = (
  yaw: number,
  pitch: number,
  distance: number,
  centre: readonly [number, number, number],
): Mat4 => {
  const eye = [
    centre[0] + distance * Math.cos(pitch) * Math.sin(yaw),
    centre[1] - distance * Math.cos(pitch) * Math.cos(yaw),
    centre[2] + distance * Math.sin(pitch),
  ];
  const forward = centre.map((c, i) => c - (eye[i] ?? 0));
  const length = Math.hypot(...forward);
  const f = forward.map((v) => v / length);
  // right = f x up, up = z
  const r = [f[1] ?? 0, -(f[0] ?? 0), 0];
  const rl = Math.hypot(...r) || 1;
  const s = r.map((v) => v / rl);
  const u = [
    (s[1] ?? 0) * (f[2] ?? 0) - (s[2] ?? 0) * (f[1] ?? 0),
    (s[2] ?? 0) * (f[0] ?? 0) - (s[0] ?? 0) * (f[2] ?? 0),
    (s[0] ?? 0) * (f[1] ?? 0) - (s[1] ?? 0) * (f[0] ?? 0),
  ];
  const dot = (a: number[], b: number[]) =>
    a.reduce((sum, v, i) => sum + v * (b[i] ?? 0), 0);
  const out = new Float32Array(16);
  out.set([
    s[0] ?? 0,
    u[0] ?? 0,
    -(f[0] ?? 0),
    0,
    s[1] ?? 0,
    u[1] ?? 0,
    -(f[1] ?? 0),
    0,
    s[2] ?? 0,
    u[2] ?? 0,
    -(f[2] ?? 0),
    0,
    -dot(s, eye),
    -dot(u, eye),
    dot(f, eye),
    1,
  ]);

  return out;
};

const compile = (gl: WebGLRenderingContext, kind: number, source: string) => {
  const shader = gl.createShader(kind);
  if (shader === null) throw new Error('no shader');
  gl.shaderSource(shader, source);
  gl.compileShader(shader);

  return shader;
};

/**
 * Lays the triangles out one by one, each corner with its triangle's normal
 * and its part's colour, so the faces read flat and sharp.
 */
const flatten = (mesh: ViewerMesh) => {
  const count = mesh.triangles.length / 3;
  const position = new Float32Array(count * 9);
  const normal = new Float32Array(count * 9);
  const colour = new Float32Array(count * 9);
  let part = 0;
  let left = mesh.parts[0]?.triangles ?? count;
  const at = (index: number, axis: number) =>
    mesh.vertices[(mesh.triangles[index] ?? 0) * 3 + axis] ?? 0;

  for (let t = 0; t < count; t += 1) {
    while (left <= 0 && part < mesh.parts.length - 1) {
      part += 1;
      left = mesh.parts[part]?.triangles ?? 0;
    }
    left -= 1;
    const tint = mesh.parts[part]?.colour ?? [0.6, 0.6, 0.6];
    const a = [at(t * 3, 0), at(t * 3, 1), at(t * 3, 2)];
    const b = [at(t * 3 + 1, 0), at(t * 3 + 1, 1), at(t * 3 + 1, 2)];
    const c = [at(t * 3 + 2, 0), at(t * 3 + 2, 1), at(t * 3 + 2, 2)];
    const e1 = b.map((v, i) => v - (a[i] ?? 0));
    const e2 = c.map((v, i) => v - (a[i] ?? 0));
    const n = [
      (e1[1] ?? 0) * (e2[2] ?? 0) - (e1[2] ?? 0) * (e2[1] ?? 0),
      (e1[2] ?? 0) * (e2[0] ?? 0) - (e1[0] ?? 0) * (e2[2] ?? 0),
      (e1[0] ?? 0) * (e2[1] ?? 0) - (e1[1] ?? 0) * (e2[0] ?? 0),
    ];
    const nl = Math.hypot(...n) || 1;
    for (const [k, corner] of [a, b, c].entries()) {
      for (let axis = 0; axis < 3; axis += 1) {
        const i = t * 9 + k * 3 + axis;
        position[i] = corner[axis] ?? 0;
        normal[i] = (n[axis] ?? 0) / nl;
        colour[i] = tint[axis] ?? 0;
      }
    }
  }

  return { position, normal, colour, count: count * 3 };
};

/** The view a viewer opens on: from the front, a little to the side and above. */
const START = { yaw: -0.6, pitch: 0.5 } as const;
const MIN_PITCH = -1.4;
const MAX_PITCH = 1.4;

/**
 * Starts a viewer on the canvas, or answers undefined where the browser has
 * no WebGL, so the page can show the object another way.
 */
export const createViewer = (
  canvas: HTMLCanvasElement,
  mesh: ViewerMesh,
): Viewer | undefined => {
  const gl = canvas.getContext('webgl', { antialias: true, alpha: true });
  if (gl === null) return undefined;

  const program = gl.createProgram();
  gl.attachShader(program, compile(gl, gl.VERTEX_SHADER, VERTEX));
  gl.attachShader(program, compile(gl, gl.FRAGMENT_SHADER, FRAGMENT));
  gl.linkProgram(program);
  if (gl.getProgramParameter(program, gl.LINK_STATUS) !== true)
    return undefined;
  gl.useProgram(program);

  const flat = flatten(mesh);
  const buffers: WebGLBuffer[] = [];
  const bind = (name: string, data: Float32Array) => {
    const buffer = gl.createBuffer();
    buffers.push(buffer);
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    const location = gl.getAttribLocation(program, name);
    gl.enableVertexAttribArray(location);
    gl.vertexAttribPointer(location, 3, gl.FLOAT, false, 0, 0);
  };
  bind('position', flat.position);
  bind('normal', flat.normal);
  bind('colour', flat.colour);
  const viewAt = gl.getUniformLocation(program, 'view');

  // The object's size frames the camera.
  let lo = [Infinity, Infinity, Infinity];
  let hi = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < mesh.vertices.length; i += 1) {
    const v = mesh.vertices[i] ?? 0;
    lo[i % 3] = Math.min(lo[i % 3] ?? 0, v);
    hi[i % 3] = Math.max(hi[i % 3] ?? 0, v);
  }
  if (!Number.isFinite(lo[0])) {
    lo = [0, 0, 0];
    hi = [1, 1, 1];
  }
  const centre = [0, 1, 2].map(
    (axis) => ((lo[axis] ?? 0) + (hi[axis] ?? 0)) / 2,
  ) as [number, number, number];
  const radius = Math.max(
    Math.hypot(
      (hi[0] ?? 0) - (lo[0] ?? 0),
      (hi[1] ?? 0) - (lo[1] ?? 0),
      (hi[2] ?? 0) - (lo[2] ?? 0),
    ) / 2,
    1,
  );
  const FOV = Math.PI / 5;
  // The camera stands where the whole sphere around the object fits, so a
  // long object seen across its diagonal never runs off the frame. A person
  // brings it nearer with the wheel.
  const fit = radius / Math.sin(FOV / 2);

  let yaw: number = START.yaw;
  let pitch: number = START.pitch;
  let distance = fit;
  let frame = 0;

  const draw = () => {
    frame = 0;
    const ratio = window.devicePixelRatio || 1;
    const width = Math.max(1, Math.round(canvas.clientWidth * ratio));
    const height = Math.max(1, Math.round(canvas.clientHeight * ratio));
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
    }
    gl.viewport(0, 0, width, height);
    gl.clearColor(0, 0, 0, 0);
    gl.enable(gl.DEPTH_TEST);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    const projection = perspective(
      FOV,
      width / height,
      distance / 100,
      distance + radius * 4,
    );
    gl.uniformMatrix4fv(
      viewAt,
      false,
      multiply(projection, orbit(yaw, pitch, distance, centre)),
    );
    gl.drawArrays(gl.TRIANGLES, 0, flat.count);
  };

  const schedule = () => {
    if (frame === 0) frame = requestAnimationFrame(draw);
  };

  // Dragging turns, a pinch of two pointers zooms.
  const pointers = new Map<number, { x: number; y: number }>();
  let pinch = 0;
  const TURN = 0.01;

  const onDown = (event: PointerEvent) => {
    canvas.setPointerCapture(event.pointerId);
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  };
  const onMove = (event: PointerEvent) => {
    const last = pointers.get(event.pointerId);
    if (last === undefined) return;
    pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      const span = Math.hypot(
        (a?.x ?? 0) - (b?.x ?? 0),
        (a?.y ?? 0) - (b?.y ?? 0),
      );
      if (pinch > 0) viewer.zoom(span / pinch);
      pinch = span;

      return;
    }
    viewer.turn(
      -(event.clientX - last.x) * TURN,
      (event.clientY - last.y) * TURN,
    );
  };
  const onUp = (event: PointerEvent) => {
    pointers.delete(event.pointerId);
    pinch = 0;
  };
  const onWheel = (event: WheelEvent) => {
    event.preventDefault();
    viewer.zoom(Math.exp(-event.deltaY * 0.001));
  };

  canvas.addEventListener('pointerdown', onDown);
  canvas.addEventListener('pointermove', onMove);
  canvas.addEventListener('pointerup', onUp);
  canvas.addEventListener('pointercancel', onUp);
  canvas.addEventListener('wheel', onWheel, { passive: false });

  const viewer: Viewer = {
    turn(dYaw, dPitch) {
      yaw += dYaw;
      pitch = Math.min(MAX_PITCH, Math.max(MIN_PITCH, pitch + dPitch));
      schedule();
    },
    zoom(factor) {
      distance = Math.min(fit * 4, Math.max(fit / 6, distance / factor));
      schedule();
    },
    reset() {
      yaw = START.yaw;
      pitch = START.pitch;
      distance = fit;
      schedule();
    },
    resize: schedule,
    destroy() {
      if (frame !== 0) cancelAnimationFrame(frame);
      canvas.removeEventListener('pointerdown', onDown);
      canvas.removeEventListener('pointermove', onMove);
      canvas.removeEventListener('pointerup', onUp);
      canvas.removeEventListener('pointercancel', onUp);
      canvas.removeEventListener('wheel', onWheel);
      for (const buffer of buffers) gl.deleteBuffer(buffer);
      gl.deleteProgram(program);
      gl.getExtension('WEBGL_lose_context')?.loseContext();
    },
  };

  draw();

  return viewer;
};

/**
 * A CSS colour as RGB from 0 to 1, read the way the browser paints it, so a
 * theme's oklch token and a file's hex colour both work. Undefined where the
 * browser cannot read it, and for an empty colour, which the canvas would
 * otherwise paint as black.
 */
export const rgbOf = (colour: string): Rgb | undefined => {
  if (colour.trim() === '') return undefined;
  const canvas = document.createElement('canvas');
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (context === null) return undefined;
  context.fillStyle = '#000';
  context.fillStyle = colour;
  context.fillRect(0, 0, 1, 1);
  const [r, g, b] = context.getImageData(0, 0, 1, 1).data;

  return [(r ?? 0) / 255, (g ?? 0) / 255, (b ?? 0) / 255];
};

/**
 * Lifts a colour that is too dark for light to shape toward a grey of that
 * brightness, so a black filament still shows its form. A colour at or above
 * the floor is left alone; a darker one keeps more of its hue the nearer it
 * is to the floor.
 */
export const liftDark = (colour: Rgb, floor = 0.4): Rgb => {
  const [r, g, b] = colour;
  const brightness = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  if (brightness >= floor) return colour;
  const mix = (floor - brightness) / floor;

  return [r + (floor - r) * mix, g + (floor - g) * mix, b + (floor - b) * mix];
};
