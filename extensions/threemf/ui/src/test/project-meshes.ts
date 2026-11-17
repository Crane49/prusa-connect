// Meshes the way get-project-mesh answers them, made by code so a
// story and a test draw a shape with depth without a file.
import type { ProjectMesh } from '../api';

/** A ring of `around` by `tube` segments, standing on z 0. */
const ring = (
  radius: number,
  thickness: number,
  around: number,
  tube: number,
  dx = 0,
): { vertices: number[]; triangles: number[] } => {
  const vertices: number[] = [];
  const triangles: number[] = [];
  for (let i = 0; i < around; i += 1) {
    const a = (i / around) * Math.PI * 2;
    for (let j = 0; j < tube; j += 1) {
      const b = (j / tube) * Math.PI * 2;
      const r = radius + thickness * Math.cos(b);
      vertices.push(
        dx + r * Math.cos(a),
        r * Math.sin(a),
        thickness + thickness * Math.sin(b),
      );
    }
  }
  for (let i = 0; i < around; i += 1) {
    for (let j = 0; j < tube; j += 1) {
      const p = i * tube + j;
      const q = ((i + 1) % around) * tube + j;
      const p2 = i * tube + ((j + 1) % tube);
      const q2 = ((i + 1) % around) * tube + ((j + 1) % tube);
      triangles.push(p, q, q2, p, q2, p2);
    }
  }

  return { vertices, triangles };
};

/** One ring of one part, printed with extruder 1. */
export const ringMesh: ProjectMesh = (() => {
  const shape = ring(20, 6, 48, 16);

  return {
    object: 0,
    vertices: shape.vertices,
    triangles: shape.triangles,
    parts: [
      { name: 'Ring', extruder: 1, triangles: shape.triangles.length / 3 },
    ],
    source_triangles: shape.triangles.length / 3,
  };
})();

/** Two linked rings, each a part of its own extruder. */
export const twoPartMesh: ProjectMesh = (() => {
  const first = ring(16, 5, 40, 14, -10);
  const second = ring(16, 5, 40, 14, 10);
  const offset = first.vertices.length / 3;

  return {
    object: 0,
    vertices: [...first.vertices, ...second.vertices],
    triangles: [
      ...first.triangles,
      ...second.triangles.map((index) => index + offset),
    ],
    parts: [
      { name: 'Left', extruder: 1, triangles: first.triangles.length / 3 },
      { name: 'Right', extruder: 2, triangles: second.triangles.length / 3 },
    ],
    source_triangles: first.triangles.length / 3 + second.triangles.length / 3,
  };
})();

/** The ring as the server sends a mesh it made coarser. */
export const simplifiedMesh: ProjectMesh = {
  ...ringMesh,
  source_triangles: 184_220,
};

/** A part too large to keep, drawn as its box. */
export const boxMesh: ProjectMesh = {
  object: 0,
  vertices: [
    -20, -10, 0, 20, -10, 0, -20, 10, 0, 20, 10, 0, -20, -10, 10, 20, -10, 10,
    -20, 10, 10, 20, 10, 10,
  ],
  triangles: [
    0, 2, 1, 1, 2, 3, 4, 5, 6, 5, 7, 6, 0, 1, 4, 1, 5, 4, 2, 6, 3, 3, 6, 7, 0,
    4, 2, 2, 4, 6, 1, 3, 5, 3, 7, 5,
  ],
  parts: [{ name: 'Huge', extruder: 1, triangles: 12 }],
  source_triangles: 2_400_000,
  box: true,
};
