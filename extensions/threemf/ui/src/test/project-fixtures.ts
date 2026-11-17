// The three projects the threemf Go package tests itself on, as get-project
// answers for them. Generated from threemf/testdata through the module's own
// projectOf, so a story draws what the server sends for those files.
import type { ProjectDocument } from '../api';

/** PrusaSlicer 2.8.1: three beds in one row, the layout before 2.9. */
export const legacyRow = {
  producer: {
    application: 'PrusaSlicer-2.8.1',
    name: 'PrusaSlicer',
    version: '2.8.1',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  counts: {
    setups: 1,
    beds: 3,
    objects: 2,
    instances: 3,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MK4S',
      printer_profile: 'Fixture MK4S',
      print_profile: 'Fixture 0.20mm',
      material_profiles: ['Fixture PLA'],
      nozzles: [
        {
          diameter_mm: 0.4,
          high_flow: false,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 250,
          y: 0,
        },
        {
          x: 250,
          y: 210,
        },
        {
          x: 0,
          y: 210,
        },
      ],
      max_print_height_mm: 220,
      technology: 'FFF',
      label_objects: 'firmware',
      binary_gcode: false,
      settings: {
        layer_height_mm: 0.3,
        first_layer_height_mm: 0.35,
        infill_percent: 20,
        infill_pattern: 'stars',
        perimeters: 3,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: false,
        filaments: [
          {
            type: 'PLA',
            colour: '#29B2B2',
          },
        ],
      },
      beds: [
        {
          index: 0,
          offset: {
            x: 0,
            y: 0,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 0,
              instance: 0,
            },
          ],
        },
        {
          index: 1,
          offset: {
            x: 300,
            y: 0,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 0,
              instance: 1,
            },
          ],
        },
        {
          index: 2,
          offset: {
            x: 600,
            y: 0,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 1,
              instance: 0,
            },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'CubeA',
      parts: [
        {
          name: 'CubeA',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 0,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 50, 50, 0],
        },
        {
          index: 1,
          item: 1,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 350, 50, 0],
        },
      ],
    },
    {
      index: 1,
      name: 'CubeB',
      parts: [
        {
          name: 'CubeB',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 2,
          },
          placement: 'inside',
          size: {
            x: 20,
            y: 10,
            z: 5,
          },
          min: {
            x: 50,
            y: 60,
          },
          outline: [
            {
              x: 50,
              y: 60,
            },
            {
              x: 70,
              y: 60,
            },
            {
              x: 70,
              y: 70,
            },
            {
              x: 50,
              y: 70,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 650, 60, 0],
        },
      ],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 16,
      height: 16,
    },
  ],
  issues: [],
} satisfies ProjectDocument;

/** PrusaSlicer 2.9.6: three beds in the 2.9 grid, with copies on each. */
export const multibed = {
  producer: {
    application: 'PrusaSlicer-2.9.6',
    name: 'PrusaSlicer',
    version: '2.9.6',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'real296',
  counts: {
    setups: 1,
    beds: 3,
    objects: 3,
    instances: 7,
    non_printable: 1,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MK4S',
      printer_profile: 'Fixture MK4S',
      print_profile: 'Fixture 0.20mm',
      material_profiles: ['Fixture PLA'],
      nozzles: [
        {
          diameter_mm: 0.4,
          high_flow: false,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 250,
          y: 0,
        },
        {
          x: 250,
          y: 210,
        },
        {
          x: 0,
          y: 210,
        },
      ],
      max_print_height_mm: 220,
      technology: 'FFF',
      label_objects: 'firmware',
      binary_gcode: false,
      settings: {
        layer_height_mm: 0.3,
        first_layer_height_mm: 0.35,
        infill_percent: 20,
        infill_pattern: 'stars',
        perimeters: 3,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: false,
        filaments: [
          {
            type: 'PLA',
            colour: '#29B2B2',
          },
        ],
      },
      beds: [
        {
          index: 0,
          offset: {
            x: 0,
            y: 0,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 0,
              instance: 0,
            },
            {
              object: 0,
              instance: 1,
            },
            {
              object: 1,
              instance: 0,
            },
            {
              object: 2,
              instance: 0,
            },
          ],
        },
        {
          index: 1,
          offset: {
            x: 347.94896630388706,
            y: 0,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 0,
              instance: 2,
            },
            {
              object: 1,
              instance: 1,
            },
          ],
        },
        {
          index: 2,
          offset: {
            x: 0,
            y: 307.94896630388706,
          },
          size: {
            x: 250,
            y: 210,
          },
          instances: [
            {
              object: 0,
              instance: 3,
            },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'CubeA',
      parts: [
        {
          name: 'CubeA',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 0,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 55, 55, 5],
        },
        {
          index: 1,
          item: 1,
          printable: false,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 100,
            y: 50,
          },
          outline: [
            {
              x: 100,
              y: 50,
            },
            {
              x: 110,
              y: 50,
            },
            {
              x: 110,
              y: 60,
            },
            {
              x: 100,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 105, 55, 5],
        },
        {
          index: 2,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 49.991033696112936,
            y: 50,
          },
          outline: [
            {
              x: 49.99,
              y: 50,
            },
            {
              x: 59.99,
              y: 50,
            },
            {
              x: 59.99,
              y: 60,
            },
            {
              x: 49.99,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 402.94, 55, 5],
        },
        {
          index: 3,
          item: 3,
          printable: true,
          bed: {
            setup: 0,
            bed: 2,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 79.99103369611294,
          },
          outline: [
            {
              x: 50,
              y: 79.99,
            },
            {
              x: 60,
              y: 79.99,
            },
            {
              x: 60,
              y: 89.99,
            },
            {
              x: 50,
              y: 89.99,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 55, 392.94, 5],
        },
      ],
    },
    {
      index: 1,
      name: 'CubeB',
      parts: [
        {
          name: 'CubeB',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 4,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 20,
            y: 10,
            z: 5,
          },
          min: {
            x: 150,
            y: 100,
          },
          outline: [
            {
              x: 150,
              y: 100,
            },
            {
              x: 170,
              y: 100,
            },
            {
              x: 170,
              y: 110,
            },
            {
              x: 150,
              y: 110,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 160, 105, 2.5],
        },
        {
          index: 1,
          item: 5,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 20,
            z: 5,
          },
          min: {
            x: 122.05103369611294,
            y: 100,
          },
          outline: [
            {
              x: 122.05,
              y: 100,
            },
            {
              x: 132.05,
              y: 100,
            },
            {
              x: 132.05,
              y: 120,
            },
            {
              x: 122.05,
              y: 120,
            },
          ],
          transform: [0, 1, 0, -1, 0, 0, 0, 0, 1, 475, 110, 2.5],
        },
      ],
    },
    {
      index: 2,
      name: 'Bracket',
      parts: [
        {
          name: 'Body',
          type: 'model',
        },
        {
          name: 'Denser',
          type: 'modifier',
        },
      ],
      instances: [
        {
          index: 0,
          item: 6,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 30,
            y: 10,
            z: 10,
          },
          min: {
            x: 100,
            y: 150,
          },
          outline: [
            {
              x: 100,
              y: 150,
            },
            {
              x: 130,
              y: 150,
            },
            {
              x: 130,
              y: 160,
            },
            {
              x: 100,
              y: 160,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 100, 150, 0],
        },
      ],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 16,
      height: 16,
    },
  ],
  issues: [],
} satisfies ProjectDocument;

/** PrusaSlicer 3.0.0-alpha12: a CORE One setup with two beds and a MINI setup with one, and a copy on no bed. */
export const twoSetups = {
  producer: {
    application: 'PrusaSlicer-3.0.0-alpha12',
    name: 'PrusaSlicer',
    version: '3.0.0-alpha12',
    format_version: '2',
    dialect: 'prusaslicer3',
    supported: true,
  },
  counts: {
    setups: 2,
    beds: 3,
    objects: 2,
    instances: 7,
    non_printable: 1,
    partly_outside: 0,
    outside: 1,
  },
  setups: [
    {
      index: 0,
      printer_model: 'COREONE',
      printer_profile: 'Prusa CORE One & CORE One+',
      print_profile: '0.20mm BALANCED @COREONE 0.4HF',
      material_profiles: ['Prusament PLA @COREONE HF0.4@COREONE 0.4'],
      nozzles: [
        {
          diameter_mm: 0.4,
          high_flow: true,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 250,
          y: 0,
        },
        {
          x: 250,
          y: 220,
        },
        {
          x: 0,
          y: 220,
        },
      ],
      max_print_height_mm: 270,
      technology: 'FFF',
      label_objects: 'firmware',
      binary_gcode: true,
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'grid',
        perimeters: 2,
        supports: 'enforcers_only',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#FF8000',
          },
        ],
      },
      beds: [
        {
          index: 0,
          offset: {
            x: 0,
            y: 0,
          },
          size: {
            x: 250,
            y: 220,
          },
          instances: [
            {
              object: 0,
              instance: 0,
            },
            {
              object: 0,
              instance: 1,
            },
            {
              object: 1,
              instance: 0,
            },
          ],
        },
        {
          index: 1,
          offset: {
            x: 270,
            y: 0,
          },
          size: {
            x: 250,
            y: 220,
          },
          instances: [
            {
              object: 0,
              instance: 2,
            },
            {
              object: 1,
              instance: 1,
            },
          ],
        },
      ],
    },
    {
      index: 1,
      printer_model: 'MINI',
      printer_profile: 'Original Prusa MINI',
      print_profile: '0.20mm SPEED @MINI 0.4',
      material_profiles: ['Prusament PLA @MINI 0.4'],
      nozzles: [
        {
          diameter_mm: 0.4,
          high_flow: false,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 180,
          y: 0,
        },
        {
          x: 180,
          y: 180,
        },
        {
          x: 0,
          y: 180,
        },
      ],
      max_print_height_mm: 180,
      technology: 'FFF',
      label_objects: 'firmware',
      binary_gcode: true,
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'grid',
        perimeters: 2,
        supports: 'enforcers_only',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#FF8000',
          },
        ],
      },
      beds: [
        {
          index: 0,
          offset: {
            x: 0,
            y: 240,
          },
          size: {
            x: 180,
            y: 180,
          },
          instances: [
            {
              object: 0,
              instance: 3,
            },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'CubeA.stl',
      parts: [
        {
          name: 'CubeA.stl',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 0,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 50, 50, 0],
        },
        {
          index: 1,
          item: 1,
          printable: false,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 100,
            y: 50,
          },
          outline: [
            {
              x: 100,
              y: 50,
            },
            {
              x: 110,
              y: 50,
            },
            {
              x: 110,
              y: 60,
            },
            {
              x: 100,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 100, 50, 0],
        },
        {
          index: 2,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 320, 50, 0],
        },
        {
          index: 3,
          item: 3,
          printable: true,
          bed: {
            setup: 1,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 10,
            z: 10,
          },
          min: {
            x: 50,
            y: 50,
          },
          outline: [
            {
              x: 50,
              y: 50,
            },
            {
              x: 60,
              y: 50,
            },
            {
              x: 60,
              y: 60,
            },
            {
              x: 50,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 50, 290, 0],
        },
      ],
    },
    {
      index: 1,
      name: 'CubeB.stl',
      parts: [
        {
          name: 'CubeB.stl',
          type: 'model',
        },
      ],
      instances: [
        {
          index: 0,
          item: 4,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 20,
            y: 10,
            z: 5,
          },
          min: {
            x: 150,
            y: 100,
          },
          outline: [
            {
              x: 150,
              y: 100,
            },
            {
              x: 170,
              y: 100,
            },
            {
              x: 170,
              y: 110,
            },
            {
              x: 150,
              y: 110,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 150, 100, 0],
        },
        {
          index: 1,
          item: 5,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 10,
            y: 20,
            z: 5,
          },
          min: {
            x: 120,
            y: 100,
          },
          outline: [
            {
              x: 120,
              y: 100,
            },
            {
              x: 130,
              y: 100,
            },
            {
              x: 130,
              y: 120,
            },
            {
              x: 120,
              y: 120,
            },
          ],
          transform: [0, 1, 0, -1, 0, 0, 0, 0, 1, 400, 100, 0],
        },
        {
          index: 2,
          item: 6,
          printable: true,
          placement: 'outside',
          size: {
            x: 20,
            y: 10,
            z: 5,
          },
          min: {
            x: 1000,
            y: 50,
          },
          outline: [
            {
              x: 1000,
              y: 50,
            },
            {
              x: 1020,
              y: 50,
            },
            {
              x: 1020,
              y: 60,
            },
            {
              x: 1000,
              y: 60,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 1000, 50, 0],
        },
      ],
    },
  ],
  thumbnails: [],
  issues: [],
} satisfies ProjectDocument;

// The projects below are drawn by hand, for the shapes the three files above
// do not have: many beds, many copies, long names, OrcaSlicer plates, a
// program the server has no reader for, and a file with no beds at all.

type Instance = ProjectDocument['objects'][number]['instances'][number];
type Setup = ProjectDocument['setups'][number];

const square = (x: number, y: number, w: number, d: number) => [
  { x, y },
  { x: x + w, y },
  { x: x + w, y: y + d },
  { x, y: y + d },
];

/** A round outline, so a story shows a hull that is not a box. */
const round = (cx: number, cy: number, r: number) =>
  Array.from({ length: 24 }, (_, at) => ({
    x: Math.round((cx + r * Math.cos((at / 24) * 2 * Math.PI)) * 100) / 100,
    y: Math.round((cy + r * Math.sin((at / 24) * 2 * Math.PI)) * 100) / 100,
  }));

const copy = (
  index: number,
  bed: { setup: number; bed: number } | undefined,
  x: number,
  y: number,
  w: number,
  d: number,
  options: Partial<Instance> & { circle?: boolean } = {},
): Instance => {
  const { circle, ...rest } = options;

  return {
    index,
    item: index,
    printable: true,
    ...(bed === undefined ? {} : { bed }),
    placement: 'inside',
    size: { x: w, y: d, z: 12 },
    min: { x, y },
    outline:
      circle === true ? round(x + w / 2, y + d / 2, w / 2) : square(x, y, w, d),
    transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, x + w / 2, y + d / 2, 0],
    ...rest,
  };
};

const prusaSetup = (beds: Setup['beds']): Setup => ({
  index: 0,
  printer_model: 'MK4S',
  printer_profile: 'Original Prusa MK4S HF0.4 nozzle',
  print_profile: '0.20mm SPEED @MK4S HF0.4',
  material_profiles: ['Prusament PLA @PG HF', 'Prusament PETG @PG HF'],
  nozzles: [{ diameter_mm: 0.4, high_flow: true }],
  bed_shape: square(0, 0, 250, 210),
  max_print_height_mm: 220,
  technology: 'FFF',
  label_objects: 'firmware',
  binary_gcode: true,
  settings: {
    layer_height_mm: 0.2,
    first_layer_height_mm: 0.2,
    infill_percent: 15,
    infill_pattern: 'gyroid',
    perimeters: 2,
    supports: 'build_plate_only',
    brim_width_mm: 0,
    wipe_tower: false,
    filaments: [
      { type: 'PLA', colour: '#FA6831' },
      { type: 'PETG', colour: '#2B2B2B' },
    ],
  },
  beds,
});

/**
 * Nine beds in the PrusaSlicer 2.9 grid, with twelve copies of a part on
 * each: 108 copies, three of them left out of the print, one sticking out of
 * its bed and one on no bed at all.
 */
export const manyBeds = ((): ProjectDocument => {
  const gap = 100;
  const grid = [
    [0, 0],
    [1, 0],
    [0, 1],
    [1, 1],
    [2, 0],
    [2, 1],
    [0, 2],
    [1, 2],
    [2, 2],
  ] as const;
  const objects: ProjectDocument['objects'] = [
    {
      index: 0,
      name: 'Hinge pin',
      parts: [{ name: 'Hinge pin', type: 'model' }],
      instances: [],
    },
    {
      index: 1,
      name: 'Cable clip',
      parts: [
        { name: 'Cable clip', type: 'model' },
        { name: 'Infill 40%', type: 'modifier' },
      ],
      instances: [],
    },
  ];
  const beds: Setup['beds'] = grid.map(([column, row], b) => ({
    index: b,
    offset: { x: column * (250 + gap), y: row * (210 + gap) },
    size: { x: 250, y: 210 },
    instances: [],
  }));

  let counter = 0;
  beds.forEach((bed, b) => {
    for (let at = 0; at < 12; at += 1) {
      const object = at % 2;
      const target = objects[object];
      if (target === undefined) continue;
      const x = 20 + (at % 4) * 55;
      const y = 20 + Math.floor(at / 4) * 60;
      const instance = copy(
        target.instances.length,
        { setup: 0, bed: b },
        x,
        y,
        object === 0 ? 30 : 40,
        object === 0 ? 30 : 25,
        {
          circle: object === 0,
          printable: !(b === 4 && at < 3),
          item: counter,
        },
      );
      counter += 1;
      target.instances.push(instance);
      bed.instances.push({ object, instance: instance.index });
    }
  });

  const pin = objects[0];
  const firstBed = beds[0];
  if (pin !== undefined && firstBed !== undefined) {
    const sticking = copy(
      pin.instances.length,
      { setup: 0, bed: 0 },
      230,
      100,
      30,
      30,
      {
        placement: 'partly_outside',
        item: counter,
        circle: true,
      },
    );
    pin.instances.push(sticking);
    firstBed.instances.push({ object: 0, instance: sticking.index });
    const lost = copy(pin.instances.length, undefined, 1100, 400, 30, 30, {
      placement: 'outside',
      item: counter + 1,
      circle: true,
    });
    pin.instances.push(lost);
  }

  const instances = objects.reduce((sum, row) => sum + row.instances.length, 0);

  return {
    producer: {
      application: 'PrusaSlicer-2.9.6',
      name: 'PrusaSlicer',
      version: '2.9.6',
      format_version: '1',
      dialect: 'prusaslicer2',
      supported: true,
    },
    title: 'Workshop spares',
    counts: {
      setups: 1,
      beds: beds.length,
      objects: objects.length,
      instances,
      non_printable: 3,
      partly_outside: 1,
      outside: 1,
    },
    setups: [prusaSetup(beds)],
    objects,
    thumbnails: [],
    issues: [],
  };
})();

const LONG =
  'Replacement_bracket_for_the_left_side_of_the_enclosure_door_v4_FINAL_really_final_with_chamfers';

/** One bed of objects whose names are far longer than any column. */
export const longNames: ProjectDocument = {
  ...multibed,
  title: LONG,
  objects: [
    {
      index: 0,
      name: LONG,
      parts: [
        { name: LONG, type: 'model' },
        { name: 'Second body', type: 'model' },
        { name: 'Hole for an M3 nut', type: 'negative_volume' },
        { name: 'Block support under the bridge', type: 'support_blocker' },
        { name: 'Paint support on the overhang', type: 'support_enforcer' },
        { name: 'Denser infill at the screw', type: 'modifier' },
      ],
      instances: [copy(0, { setup: 0, bed: 0 }, 30, 40, 180, 60)],
    },
    {
      index: 1,
      name: `${LONG}_mirrored_copy_for_the_right_side`,
      parts: [{ name: 'body', type: 'model' }],
      instances: [
        copy(0, { setup: 0, bed: 0 }, 30, 120, 180, 60, { printable: false }),
      ],
    },
  ],
  setups: [
    prusaSetup([
      {
        index: 0,
        offset: { x: 0, y: 0 },
        size: { x: 250, y: 210 },
        instances: [
          { object: 0, instance: 0 },
          { object: 1, instance: 0 },
        ],
      },
    ]),
  ],
  counts: {
    setups: 1,
    beds: 1,
    objects: 2,
    instances: 2,
    non_printable: 1,
    partly_outside: 0,
    outside: 0,
  },
  thumbnails: [],
  issues: [],
};

const orcaBeds = (count: number): Setup['beds'] =>
  Array.from({ length: count }, (_, b) => ({
    index: b,
    name: ['Body', 'Lid', '', 'Hinges and small parts', '', 'Spares'][b] ?? '',
    locked: b === 1,
    offset: { x: (b % 3) * 307.2, y: -Math.floor(b / 3) * 307.2 },
    size: { x: 256, y: 256 },
    thumbnail: b,
    instances: [],
  }));

const orcaProject = (supported: boolean): ProjectDocument => {
  const beds = orcaBeds(6);
  const objects: ProjectDocument['objects'] = beds.map((bed, b) => {
    const parts = 1 + (b % 3);
    const object = {
      index: b,
      name: `Plate ${String(b + 1)} part`,
      parts: [{ name: 'body', type: 'model' as const }],
      instances: Array.from({ length: parts }, (_, at) =>
        copy(at, { setup: 0, bed: b }, 30 + at * 70, 60, 60, 120 - at * 20),
      ),
    };
    object.instances.forEach((_, at) => {
      bed.instances.push({ object: b, instance: at });
    });

    return object;
  });

  return {
    producer: supported
      ? {
          application: 'OrcaSlicer-2.2.0',
          name: 'OrcaSlicer',
          version: '2.2.0',
          format_version: '1',
          dialect: 'orcaslicer',
          supported: true,
        }
      : {
          application: 'BambuStudio-01.10.01.50',
          name: 'BambuStudio',
          version: '01.10.01.50',
          format_version: '1',
          dialect: 'orcaslicer',
          supported: false,
        },
    counts: {
      setups: 1,
      beds: 6,
      objects: objects.length,
      instances: objects.reduce((sum, row) => sum + row.instances.length, 0),
      non_printable: 0,
      partly_outside: 0,
      outside: 0,
    },
    setups: [
      {
        index: 0,
        printer_model: supported ? 'Elegoo Neptune 4' : 'Bambu Lab X1 Carbon',
        printer_profile: supported
          ? 'Elegoo Neptune 4 (0.4 nozzle)'
          : 'Bambu Lab X1 Carbon 0.4 nozzle',
        print_profile: '0.20mm Standard',
        material_profiles: ['Generic PLA'],
        nozzles: [{ diameter_mm: 0.4 }],
        bed_shape: square(0, 0, 256, 256),
        max_print_height_mm: 250,
        technology: 'FFF',
        settings: {
          layer_height_mm: 0.2,
          first_layer_height_mm: 0.2,
          infill_percent: 15,
          infill_pattern: 'crosshatch',
          perimeters: 3,
          supports: 'none',
          wipe_tower: false,
          filaments: [{ type: 'PLA', colour: '#26A69A' }],
        },
        beds,
      },
    ],
    objects,
    thumbnails: beds.map((_, b) => ({
      index: b,
      bed: { setup: 0, bed: b },
      media_type: 'image/png' as const,
      width: 512,
      height: 512,
    })),
    issues: supported
      ? []
      : [{ code: 'unsupported_producer', detail: 'BambuStudio-01.10.01.50' }],
  };
};

/** Six OrcaSlicer plates in two rows, named, one of them locked. */
export const orcaPlates = orcaProject(true);

/** The same plates saved by BambuStudio, which the server reads but does not support. */
export const bambuPlates = orcaProject(false);

/**
 * A plain 3MF from a program the server has no reader for: one bed of unknown
 * size, no settings, and the copies placed where the file put them.
 */
export const generic: ProjectDocument = {
  producer: {
    application: 'UltiMaker Cura 5.7.2',
    name: 'UltiMaker Cura',
    version: '5.7.2',
    dialect: 'generic',
    supported: true,
  },
  title: 'Desk organiser',
  counts: {
    setups: 1,
    beds: 1,
    objects: 2,
    instances: 2,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      material_profiles: [],
      nozzles: [],
      bed_shape: [],
      settings: { filaments: [] },
      beds: [
        {
          index: 0,
          offset: { x: 0, y: 0 },
          size: { x: 0, y: 0 },
          instances: [
            { object: 0, instance: 0 },
            { object: 1, instance: 0 },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'Tray',
      parts: [{ name: 'Tray', type: 'model' }],
      instances: [
        copy(0, { setup: 0, bed: 0 }, -80, -40, 110, 80, {
          placement: 'unknown',
        }),
      ],
    },
    {
      index: 1,
      name: 'Pen cup',
      parts: [{ name: 'Pen cup', type: 'model' }],
      instances: [
        copy(0, { setup: 0, bed: 0 }, 50, -30, 60, 60, {
          placement: 'unknown',
          circle: true,
        }),
      ],
    },
  ],
  thumbnails: [
    { index: 0, media_type: 'image/png', width: 1024, height: 1024 },
  ],
  issues: [],
};

/** A project whose settings file could not be read and whose mesh was too large. */
export const withIssues: ProjectDocument = {
  ...multibed,
  issues: [
    { code: 'settings_unreadable', detail: 'Metadata/Slic3r_PE.config' },
    { code: 'outline_approx', detail: 'object 2' },
    { code: 'outline_approx', detail: 'object 2' },
    { code: 'thumbnail_unreadable', detail: 'Metadata/thumbnail.png' },
  ],
};

/** What the server answers for a file that is not a 3MF project. */
export const notAProject = {
  code: 'threemf/NOT_A_PROJECT',
  message:
    'threemf/NOT_A_PROJECT: the file is not a 3MF project the server can read',
  status: 422,
};

/** A picture of a bed, drawn rather than fetched, so a story needs no server. */
export const plateThumbnail = (index: number): string =>
  'data:image/svg+xml;utf8,' +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 120">' +
      '<rect width="120" height="120" fill="#2b2b2b"/>' +
      `<rect x="${String(14 + (index % 3) * 10)}" y="20" width="40" height="30" fill="#fa6831"/>` +
      '<circle cx="80" cy="80" r="18" fill="#fa6831"/>' +
      '</svg>',
  );

/**
 * Three beds of letters, the way a person parks what they do not print: a
 * large sign left out of the print and moved off the beds behind them, a
 * letter left out and half off its bed, an empty middle bed, and a letter
 * whose long name is wider than its outline.
 */
export const parked: ProjectDocument = (() => {
  const beds: Setup['beds'] = [0, 1, 2].map((b) => ({
    index: b,
    offset: { x: b * 300, y: 0 },
    size: { x: 250, y: 210 },
    instances: [],
  }));
  const letters = ['A', 'B', 'C', 'D', 'E'];
  const objects: ProjectDocument['objects'] = letters.map((letter, o) => ({
    index: o,
    name: `8 BIT LETTER ${letter}.stl`,
    parts: [{ name: `8 BIT LETTER ${letter}.stl`, type: 'model' as const }],
    instances: [],
  }));
  const add = (
    o: number,
    bed: number | undefined,
    instance: Instance,
  ): void => {
    objects[o]?.instances.push(instance);
    if (bed !== undefined) {
      beds[bed]?.instances.push({ object: o, instance: instance.index });
    }
  };
  add(0, 0, copy(0, { setup: 0, bed: 0 }, 20, 20, 40, 40));
  add(1, 0, copy(0, { setup: 0, bed: 0 }, 80, 20, 40, 40));
  // A letter left out of the print that sticks out of the right of its bed.
  add(
    2,
    0,
    copy(0, { setup: 0, bed: 0 }, 225, 120, 40, 40, {
      printable: false,
      placement: 'partly_outside',
    }),
  );
  add(3, 2, copy(0, { setup: 0, bed: 2 }, 20, 20, 40, 40));
  // A narrow letter whose long name cannot fit inside it.
  add(4, 2, copy(0, { setup: 0, bed: 2 }, 100, 20, 14, 60));
  objects.push({
    index: 5,
    name: '8 BIT LETTERS AND NUMBERS COMPLETE SET sign.stl',
    parts: [{ name: 'sign', type: 'model' }],
    instances: [
      copy(0, undefined, 60, 400, 600, 90, {
        printable: false,
        placement: 'outside',
        item: 9,
      }),
    ],
  });

  return {
    producer: {
      application: 'PrusaSlicer-2.5.0',
      name: 'PrusaSlicer',
      version: '2.5.0',
      format_version: '1',
      dialect: 'prusaslicer2',
      supported: true,
    },
    title: '8 bit letters',
    counts: {
      setups: 1,
      beds: 3,
      objects: objects.length,
      instances: 6,
      non_printable: 2,
      partly_outside: 1,
      outside: 1,
    },
    setups: [prusaSetup(beds)],
    objects,
    thumbnails: [],
    issues: [],
  };
})();

/** The same beds, with every copy on the third bed left out of the print. */
export const parkedBed: ProjectDocument = {
  ...parked,
  objects: parked.objects.map((object) => ({
    ...object,
    instances: object.instances.map((instance) =>
      instance.bed?.bed === 2 ? { ...instance, printable: false } : instance,
    ),
  })),
  counts: { ...parked.counts, non_printable: 4 },
};
