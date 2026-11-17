// Multi-material projects as get-project answers for them: extruders,
// painting, layer ranges, layer changes and a file of G-code alone. Generated
// from public projects through the module's own document, with the titles
// and a few names replaced and each outline cut to 12 points.
import type { ProjectDocument } from '../api';

/** PrusaSlicer 2.9 on an MK4S with an MMU3: three objects, each part on its own extruder of five. */
export const mmuObjects: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.9.2',
    name: 'PrusaSlicer',
    version: '2.9.2',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Colour test objects',
  counts: {
    setups: 1,
    beds: 1,
    objects: 3,
    instances: 3,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MK4SMMU3',
      printer_profile: 'Original Prusa MK4S MMU3 0.4 nozzle',
      print_profile: '0.20mm SPEED @MK4S 0.4',
      material_profiles: [
        'Prusament PLA @MK4S',
        'Prusament PLA @MK4S',
        'Prusament PLA @MK4S',
        'Prusament PLA @MK4S',
        'Prusament PLA @MK4S',
      ],
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
      binary_gcode: true,
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'grid',
        perimeters: 2,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#FF8000',
          },
          {
            type: 'PLA',
            colour: '#DB5182',
          },
          {
            type: 'PLA',
            colour: '#3EC0FF',
          },
          {
            type: 'PLA',
            colour: '#FF4F4F',
          },
          {
            type: 'PLA',
            colour: '#FBEB7D',
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
              object: 1,
              instance: 0,
            },
            {
              object: 2,
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
      name: 'MMU_2_COLOR_TEST.stl',
      parts: [
        {
          name: 'MMU_2_COLOR_TEST.stl_1',
          type: 'model',
          extruder: 1,
        },
        {
          name: 'MMU_2_COLOR_TEST.stl_1_2',
          type: 'model',
          extruder: 2,
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
            x: 29.99664306640625,
            y: 30,
            z: 2,
          },
          min: {
            x: 142.60839946679687,
            y: 112.65454,
          },
          outline: [
            {
              x: 142.61,
              y: 127.43,
            },
            {
              x: 144.4,
              y: 120.55,
            },
            {
              x: 149.16,
              y: 115.26,
            },
            {
              x: 157.61,
              y: 112.65,
            },
            {
              x: 164.52,
              y: 114.34,
            },
            {
              x: 170.13,
              y: 119.39,
            },
            {
              x: 172.61,
              y: 127.88,
            },
            {
              x: 170.82,
              y: 134.76,
            },
            {
              x: 166.06,
              y: 140.05,
            },
            {
              x: 157.61,
              y: 142.65,
            },
            {
              x: 150.7,
              y: 140.97,
            },
            {
              x: 145.09,
              y: 135.92,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 157.606721, 127.65454, 1],
        },
      ],
      extruders: [1, 2],
    },
    {
      index: 1,
      name: 'MMU_5COLOR_TEST.stl',
      parts: [
        {
          name: 'MMU_5COLOR_TEST.stl_1',
          type: 'model',
          extruder: 1,
        },
        {
          name: 'MMU_5COLOR_TEST.stl_1_2',
          type: 'model',
          extruder: 2,
        },
        {
          name: 'MMU_5COLOR_TEST.stl_1_3',
          type: 'model',
          extruder: 3,
        },
        {
          name: 'MMU_5COLOR_TEST.stl_1_4',
          type: 'model',
          extruder: 4,
        },
        {
          name: 'MMU_5COLOR_TEST.stl_1_5',
          type: 'model',
          extruder: 5,
        },
      ],
      instances: [
        {
          index: 0,
          item: 1,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 29.99810791015625,
            y: 29.99810791015625,
            z: 2,
          },
          min: {
            x: 86.99154404492188,
            y: 89.82561704492187,
          },
          outline: [
            {
              x: 86.99,
              y: 104.66,
            },
            {
              x: 88.7,
              y: 97.87,
            },
            {
              x: 93.59,
              y: 92.4,
            },
            {
              x: 102.16,
              y: 89.83,
            },
            {
              x: 108.95,
              y: 91.54,
            },
            {
              x: 114.42,
              y: 96.42,
            },
            {
              x: 116.99,
              y: 104.99,
            },
            {
              x: 115.28,
              y: 111.78,
            },
            {
              x: 110.39,
              y: 117.25,
            },
            {
              x: 101.82,
              y: 119.82,
            },
            {
              x: 95.03,
              y: 118.11,
            },
            {
              x: 89.57,
              y: 113.23,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 101.990598, 104.824671, 1],
        },
      ],
      extruders: [1, 2, 3, 4, 5],
    },
    {
      index: 2,
      name: 'MMU_3_COLOR_TEST.stl',
      parts: [
        {
          name: 'MMU_3_COLOR_TEST.stl_1',
          type: 'model',
          extruder: 1,
        },
        {
          name: 'MMU_3_COLOR_TEST.stl_1_2',
          type: 'model',
          extruder: 2,
        },
        {
          name: 'MMU_3_COLOR_TEST.stl_1_3',
          type: 'model',
          extruder: 3,
        },
      ],
      instances: [
        {
          index: 0,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 29.997291564941392,
            y: 30.000000000000014,
            z: 2,
          },
          min: {
            x: 111.1885912175293,
            y: 111.917253,
          },
          outline: [
            {
              x: 111.19,
              y: 126.72,
            },
            {
              x: 113,
              y: 119.77,
            },
            {
              x: 117.83,
              y: 114.46,
            },
            {
              x: 126.19,
              y: 111.92,
            },
            {
              x: 133.16,
              y: 113.64,
            },
            {
              x: 138.76,
              y: 118.73,
            },
            {
              x: 141.19,
              y: 127.12,
            },
            {
              x: 139.37,
              y: 134.07,
            },
            {
              x: 134.54,
              y: 139.38,
            },
            {
              x: 126.19,
              y: 141.92,
            },
            {
              x: 119.22,
              y: 140.2,
            },
            {
              x: 113.62,
              y: 135.1,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 126.187237, 126.917253, 1],
        },
      ],
      extruders: [1, 2, 3],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [1, 2, 3, 4, 5],
};

/** PrusaSlicer 2.7 with an MMU2S: one figure painted in four colours. */
export const mmuPainted: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.7.1+win64',
    name: 'PrusaSlicer',
    version: '2.7.1',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Painted figure',
  counts: {
    setups: 1,
    beds: 1,
    objects: 1,
    instances: 1,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MK3SMMU3',
      printer_profile: 'Original Prusa i3 MK3S & MK3S+ MMU3 - Copy',
      print_profile: '0.20mm QUALITY @MK3 - MMU3_rev03',
      material_profiles: [
        'Generic PLA @MMU',
        'Generic PLA @MMU',
        'Generic PLA @MMU',
        'Generic PLA @MMU',
        'Generic PLA @MMU',
      ],
      nozzles: [
        {
          diameter_mm: 0.4,
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
      max_print_height_mm: 210,
      technology: 'FFF',
      label_objects: 'octoprint',
      binary_gcode: false,
      settings: {
        layer_height_mm: 0.15,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'build_plate_only',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#C0C0C0',
          },
          {
            type: 'PLA',
            colour: '#9E1AE6',
          },
          {
            type: 'PLA',
            colour: '#000000',
          },
          {
            type: 'PLA',
            colour: '#FFFFFF',
          },
          {
            type: 'PLA',
            colour: '#FFFF00',
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
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'Figure.stl',
      parts: [
        {
          name: 'Figure.stl',
          type: 'model',
          extruder: 1,
          painted_extruders: [1, 2, 3, 4],
          painting: {
            multimaterial: true,
          },
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
            x: 129.1097451131601,
            y: 124.11488761958799,
            z: 137.39828537016513,
          },
          min: {
            x: 70.24572344341996,
            y: 15.863850190205987,
          },
          outline: [
            {
              x: 70.25,
              y: 26.28,
            },
            {
              x: 75.88,
              y: 16.11,
            },
            {
              x: 198.35,
              y: 37.6,
            },
            {
              x: 155.3,
              y: 124.02,
            },
            {
              x: 149.92,
              y: 129.79,
            },
            {
              x: 141.03,
              y: 135.38,
            },
            {
              x: 129.18,
              y: 139.4,
            },
            {
              x: 117.21,
              y: 139.83,
            },
            {
              x: 106.18,
              y: 138.01,
            },
            {
              x: 93.95,
              y: 133.83,
            },
            {
              x: 86.64,
              y: 129.06,
            },
            {
              x: 82.08,
              y: 122.51,
            },
          ],
          transform: [
            1.43522383, 0, 0, 0, -3.18683709e-16, 1.43522383, 0, -1.43522383,
            -3.18683709e-16, 134.800596, 77.921294, 68.6991428,
          ],
        },
      ],
      extruders: [1, 2, 3, 4],
      painting: {
        multimaterial: true,
      },
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [1, 2, 3, 4],
};

/** PrusaSlicer 2.9 on a CORE One INDX: one frog painted with four tools. */
export const indxPainted: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.9.6',
    name: 'PrusaSlicer',
    version: '2.9.6',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Painted frog',
  counts: {
    setups: 1,
    beds: 1,
    objects: 1,
    instances: 1,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'COREONE_INDX4T',
      printer_profile: 'Prusa CORE One INDX 4T HF0.4 nozzle',
      print_profile: '0.20mm Balanced ★ @COREONEINDX HF0.4',
      material_profiles: [
        'Prusament PLA @COREONEINDX HF0.4',
        'Prusament PLA @COREONEINDX HF0.4',
        'Prusament PLA @COREONEINDX HF0.4',
        'Prusament PLA @COREONEINDX HF0.4',
      ],
      nozzles: [
        {
          diameter_mm: 0.4,
          high_flow: true,
        },
        {
          diameter_mm: 0.4,
          high_flow: true,
        },
        {
          diameter_mm: 0.4,
          high_flow: true,
        },
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
          x: 248,
          y: 0,
        },
        {
          x: 248,
          y: 205,
        },
        {
          x: 0,
          y: 205,
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
        wipe_tower: false,
        filaments: [
          {
            type: 'PLA',
            colour: '#F58231',
          },
          {
            type: 'PLA',
            colour: '#000000',
          },
          {
            type: 'PLA',
            colour: '#0000FF',
          },
          {
            type: 'PLA',
            colour: '#ECEC00',
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
            x: 248,
            y: 205,
          },
          instances: [
            {
              object: 0,
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
      name: 'Treefrog.stl',
      parts: [
        {
          name: 'Treefrog.stl',
          type: 'model',
          extruder: 1,
          painted_extruders: [1, 2, 3, 4],
          painting: {
            multimaterial: true,
          },
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
            x: 102.45999908447266,
            y: 90.41999816894531,
            z: 50.014678955078125,
          },
          min: {
            x: 72.77000045776367,
            y: 57.290000915527344,
          },
          outline: [
            {
              x: 72.77,
              y: 77.01,
            },
            {
              x: 78.51,
              y: 63.51,
            },
            {
              x: 81.03,
              y: 61.13,
            },
            {
              x: 123.51,
              y: 57.31,
            },
            {
              x: 166.05,
              y: 60.97,
            },
            {
              x: 168.53,
              y: 62.09,
            },
            {
              x: 175.07,
              y: 76.17,
            },
            {
              x: 169.81,
              y: 105.79,
            },
            {
              x: 141.15,
              y: 146.01,
            },
            {
              x: 137.05,
              y: 147.71,
            },
            {
              x: 108.17,
              y: 147.05,
            },
            {
              x: 78.49,
              y: 106.29,
            },
          ],
          transform: [2, 0, 0, 0, 2, 0, 0, 0, 2, 124, 102.5, 25.0073395],
        },
      ],
      extruders: [1, 2, 3, 4],
      painting: {
        multimaterial: true,
      },
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [1, 2, 3, 4],
};

/** BambuStudio with an AMS: one object of three parts, painted in colours and with a seam. */
export const orcaPainted: ProjectDocument = {
  producer: {
    application: 'BambuStudio-01.08.04.51',
    name: 'BambuStudio',
    version: '01.08.04.51',
    format_version: '1',
    dialect: 'orcaslicer',
    supported: false,
  },
  title: 'Trophy',
  counts: {
    setups: 1,
    beds: 1,
    objects: 1,
    instances: 1,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'Bambu Lab P1S',
      printer_profile: 'Bambu Lab P1S 0.4 nozzle',
      print_profile: '0.20mm Gyroid',
      material_profiles: [
        'Bambu PLA Basic @BBL X1C',
        'Bambu PLA Basic @BBL X1C(Golf Trophy with Crown.3mf)',
        'Bambu PLA Basic @BBL X1C(Golf Trophy with Crown.3mf)',
        'Bambu PLA Silk 6 max volumetric',
      ],
      nozzles: [
        {
          diameter_mm: 0.4,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 256,
          y: 0,
        },
        {
          x: 256,
          y: 256,
        },
        {
          x: 0,
          y: 256,
        },
      ],
      max_print_height_mm: 250,
      technology: 'FFF',
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 5,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'none',
        brim_width_mm: 5,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#000000',
          },
          {
            type: 'PLA',
            colour: '#FFFFFF',
          },
          {
            type: 'PLA',
            colour: '#000000',
          },
          {
            type: 'PLA',
            colour: '#FFC600',
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
            x: 256,
            y: 256,
          },
          thumbnail: 0,
          instances: [
            {
              object: 0,
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
      name: 'Assembly',
      parts: [
        {
          name: 'GolfTrophy.stl',
          type: 'model',
          extruder: 4,
          painted_extruders: [1, 2],
          painting: {
            multimaterial: true,
            seams: true,
          },
        },
        {
          name: 'crown solo.stl',
          type: 'model',
          extruder: 4,
          painted_extruders: [2],
          painting: {
            multimaterial: true,
            seams: true,
          },
        },
        {
          name: 'text_shape',
          type: 'model',
          extruder: 1,
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
            x: 99.999999598126,
            y: 100.0103341102008,
            z: 157.18883015558325,
          },
          min: {
            x: 78.00000020093701,
            y: 77.9948325448996,
          },
          outline: [
            {
              x: 78,
              y: 126.59,
            },
            {
              x: 83.66,
              y: 104.85,
            },
            {
              x: 100.57,
              y: 86.17,
            },
            {
              x: 127.28,
              y: 77.99,
            },
            {
              x: 151.76,
              y: 84,
            },
            {
              x: 170.2,
              y: 101.18,
            },
            {
              x: 178,
              y: 128,
            },
            {
              x: 171.65,
              y: 152.39,
            },
            {
              x: 156.58,
              y: 169.03,
            },
            {
              x: 127.28,
              y: 178.01,
            },
            {
              x: 105.47,
              y: 172.66,
            },
            {
              x: 86.55,
              y: 156,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 128, 152.679216, 78.4821684],
        },
      ],
      extruders: [1, 2, 4],
      painting: {
        multimaterial: true,
        seams: true,
      },
    },
  ],
  thumbnails: [
    {
      index: 0,
      bed: {
        setup: 0,
        bed: 0,
      },
      media_type: 'image/png',
      width: 512,
      height: 512,
    },
  ],
  issues: [
    {
      code: 'unsupported_producer',
      detail: 'BambuStudio-01.08.04.51',
    },
  ],
  extruders: [1, 2, 4],
};

/** BambuStudio: three plates of painted figures, one with painted supports. */
export const orcaPlatesPainted: ProjectDocument = {
  producer: {
    application: 'BambuStudio-01.07.03.50',
    name: 'BambuStudio',
    version: '01.07.03.50',
    format_version: '1',
    dialect: 'orcaslicer',
    supported: false,
  },
  counts: {
    setups: 1,
    beds: 3,
    objects: 6,
    instances: 6,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'Bambu Lab X1 Carbon',
      printer_profile: 'Bambu Lab X1 Carbon 0.4 nozzle',
      print_profile: '0.20mm Standard @BBL X1C',
      material_profiles: [
        'Generic PLA',
        'Generic PLA',
        'Generic PLA',
        'Generic PLA',
      ],
      nozzles: [
        {
          diameter_mm: 0.4,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 256,
          y: 0,
        },
        {
          x: 256,
          y: 256,
        },
        {
          x: 0,
          y: 256,
        },
      ],
      max_print_height_mm: 250,
      technology: 'FFF',
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'build_plate_only',
        brim_width_mm: 5,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#161616',
          },
          {
            type: 'PLA',
            colour: '#FFFFFF',
          },
          {
            type: 'PLA',
            colour: '#B7CB69',
          },
          {
            type: 'PLA',
            colour: '#F90007',
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
            x: 256,
            y: 256,
          },
          thumbnail: 0,
          instances: [
            {
              object: 0,
              instance: 0,
            },
            {
              object: 4,
              instance: 0,
            },
          ],
        },
        {
          index: 1,
          offset: {
            x: 307.2,
            y: 0,
          },
          size: {
            x: 256,
            y: 256,
          },
          thumbnail: 1,
          instances: [
            {
              object: 2,
              instance: 0,
            },
            {
              object: 5,
              instance: 0,
            },
          ],
        },
        {
          index: 2,
          offset: {
            x: 0,
            y: -307.2,
          },
          size: {
            x: 256,
            y: 256,
          },
          thumbnail: 2,
          instances: [
            {
              object: 1,
              instance: 0,
            },
            {
              object: 3,
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
      name: 'helmet.obj',
      parts: [
        {
          name: 'helmet.obj',
          type: 'model',
          extruder: 2,
          painted_extruders: [1, 4],
          painting: {
            multimaterial: true,
          },
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
            x: 73.08300018310547,
            y: 64.09949889214397,
            z: 66.11533743160327,
          },
          min: {
            x: 104.02847090844728,
            y: 139.2963590316426,
          },
          outline: [
            {
              x: 104.03,
              y: 165.8,
            },
            {
              x: 123.01,
              y: 145,
            },
            {
              x: 146.3,
              y: 139.85,
            },
            {
              x: 176.35,
              y: 158.89,
            },
            {
              x: 175.73,
              y: 175.51,
            },
            {
              x: 168.38,
              y: 189.79,
            },
            {
              x: 161.04,
              y: 196.83,
            },
            {
              x: 148.65,
              y: 202.43,
            },
            {
              x: 138.02,
              y: 203.31,
            },
            {
              x: 124.76,
              y: 199.61,
            },
            {
              x: 117.11,
              y: 194.43,
            },
            {
              x: 111.59,
              y: 188.17,
            },
          ],
          transform: [
            1.5, 0, 0, 0, 0.389762255, 1.44847692, 0, -1.44847692, 0.389762255,
            140.569971, 174.681261, 29.7876391,
          ],
        },
      ],
      extruders: [1, 2, 4],
      painting: {
        multimaterial: true,
      },
    },
    {
      index: 1,
      name: 'fullbody.obj',
      parts: [
        {
          name: 'fullbody.obj',
          type: 'model',
          extruder: 4,
          painted_extruders: [1, 2, 4],
          painting: {
            multimaterial: true,
          },
        },
      ],
      instances: [
        {
          index: 0,
          item: 1,
          printable: true,
          bed: {
            setup: 0,
            bed: 2,
          },
          placement: 'inside',
          size: {
            x: 59.193002700805664,
            y: 37.46970660134102,
            z: 52.290888067508554,
          },
          min: {
            x: 98.40349864959717,
            y: 133.0000000441404,
          },
          outline: [
            {
              x: 98.4,
              y: 151,
            },
            {
              x: 99.57,
              y: 147.13,
            },
            {
              x: 112.75,
              y: 135.43,
            },
            {
              x: 129.83,
              y: 133.08,
            },
            {
              x: 146.36,
              y: 137.37,
            },
            {
              x: 157.41,
              y: 149.62,
            },
            {
              x: 157.32,
              y: 153.34,
            },
            {
              x: 156.64,
              y: 155.3,
            },
            {
              x: 132.74,
              y: 169.96,
            },
            {
              x: 126.07,
              y: 170.38,
            },
            {
              x: 101.32,
              y: 157.71,
            },
            {
              x: 99.06,
              y: 154.62,
            },
          ],
          transform: [
            1.5, 0, 0, 0, 0.00158015828, 1.49999917, 0, -1.49999917,
            0.00158015828, 128, -155.484758, 26.1505258,
          ],
        },
      ],
      extruders: [1, 2, 4],
      painting: {
        multimaterial: true,
      },
    },
    {
      index: 2,
      name: 'head3.obj',
      parts: [
        {
          name: 'head3.obj',
          type: 'model',
          extruder: 1,
          painted_extruders: [1, 2, 3, 4],
          painting: {
            multimaterial: true,
            supports: true,
          },
        },
      ],
      instances: [
        {
          index: 0,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 37.97412791928724,
            y: 41.11739058948427,
            z: 62.412901876696765,
          },
          min: {
            x: 141.87770405992086,
            y: 109.68302875103927,
          },
          outline: [
            {
              x: 141.88,
              y: 132.95,
            },
            {
              x: 151.43,
              y: 113.07,
            },
            {
              x: 153.85,
              y: 110.86,
            },
            {
              x: 156.21,
              y: 109.88,
            },
            {
              x: 158.25,
              y: 109.69,
            },
            {
              x: 167.51,
              y: 111.59,
            },
            {
              x: 177.72,
              y: 118.61,
            },
            {
              x: 179.1,
              y: 120.56,
            },
            {
              x: 179.62,
              y: 121.88,
            },
            {
              x: 177.66,
              y: 140.32,
            },
            {
              x: 168.67,
              y: 149.04,
            },
            {
              x: 148.08,
              y: 146.08,
            },
          ],
          transform: [
            1.44151898, 0.414727913, 0.00487706052, -0.0752875274, 0.244304354,
            1.4780552, 0.407866175, -1.42067454, 0.255595469, 468.837654,
            129.281448, 30.4912472,
          ],
        },
      ],
      extruders: [1, 2, 3, 4],
      painting: {
        multimaterial: true,
        supports: true,
      },
    },
    {
      index: 3,
      name: 'fullbody.obj',
      parts: [
        {
          name: 'fullbody.obj',
          type: 'model',
          extruder: 4,
          painted_extruders: [1, 2, 4],
          painting: {
            multimaterial: true,
          },
        },
      ],
      instances: [
        {
          index: 0,
          item: 3,
          printable: true,
          bed: {
            setup: 0,
            bed: 2,
          },
          placement: 'inside',
          size: {
            x: 59.193002700805664,
            y: 37.46970660134102,
            z: 52.290888067508554,
          },
          min: {
            x: 98.40349864959717,
            y: 85.53029504414039,
          },
          outline: [
            {
              x: 98.4,
              y: 103.53,
            },
            {
              x: 99.57,
              y: 99.66,
            },
            {
              x: 112.75,
              y: 87.96,
            },
            {
              x: 129.83,
              y: 85.61,
            },
            {
              x: 146.36,
              y: 89.9,
            },
            {
              x: 157.41,
              y: 102.15,
            },
            {
              x: 157.32,
              y: 105.87,
            },
            {
              x: 156.64,
              y: 107.83,
            },
            {
              x: 132.74,
              y: 122.49,
            },
            {
              x: 126.07,
              y: 122.91,
            },
            {
              x: 101.32,
              y: 110.24,
            },
            {
              x: 99.06,
              y: 107.16,
            },
          ],
          transform: [
            1.5, 0, 0, 0, 0.00158015828, 1.49999917, 0, -1.49999917,
            0.00158015828, 128, -202.954463, 26.1505258,
          ],
        },
      ],
      extruders: [1, 2, 4],
      painting: {
        multimaterial: true,
      },
    },
    {
      index: 4,
      name: 'helmet.obj',
      parts: [
        {
          name: 'helmet.obj',
          type: 'model',
          extruder: 2,
          painted_extruders: [1, 4],
          painting: {
            multimaterial: true,
          },
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
            x: 73.08300018310547,
            y: 64.09949889214397,
            z: 66.11533743160327,
          },
          min: {
            x: 78.88879290844727,
            y: 52.60403103164259,
          },
          outline: [
            {
              x: 78.89,
              y: 79.11,
            },
            {
              x: 97.87,
              y: 58.3,
            },
            {
              x: 121.16,
              y: 53.16,
            },
            {
              x: 151.21,
              y: 72.19,
            },
            {
              x: 150.59,
              y: 88.81,
            },
            {
              x: 143.24,
              y: 103.1,
            },
            {
              x: 135.9,
              y: 110.14,
            },
            {
              x: 123.51,
              y: 115.74,
            },
            {
              x: 112.88,
              y: 116.62,
            },
            {
              x: 99.62,
              y: 112.92,
            },
            {
              x: 91.97,
              y: 107.74,
            },
            {
              x: 86.45,
              y: 101.48,
            },
          ],
          transform: [
            1.5, 0, 0, 0, 0.389762255, 1.44847692, 0, -1.44847692, 0.389762255,
            115.430293, 87.988933, 29.7876391,
          ],
        },
      ],
      extruders: [1, 2, 4],
      painting: {
        multimaterial: true,
      },
    },
    {
      index: 5,
      name: 'head3.obj',
      parts: [
        {
          name: 'head3.obj',
          type: 'model',
          extruder: 1,
          painted_extruders: [1, 2, 3, 4],
          painting: {
            multimaterial: true,
            supports: true,
          },
        },
      ],
      instances: [
        {
          index: 0,
          item: 5,
          printable: true,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 37.97412791928724,
            y: 41.11739058948429,
            z: 62.412901876696765,
          },
          min: {
            x: 78.54926105992087,
            y: 105.18818075103927,
          },
          outline: [
            {
              x: 78.55,
              y: 128.45,
            },
            {
              x: 88.1,
              y: 108.58,
            },
            {
              x: 90.53,
              y: 106.36,
            },
            {
              x: 92.89,
              y: 105.39,
            },
            {
              x: 94.92,
              y: 105.2,
            },
            {
              x: 104.18,
              y: 107.09,
            },
            {
              x: 114.4,
              y: 114.12,
            },
            {
              x: 115.77,
              y: 116.06,
            },
            {
              x: 116.29,
              y: 117.39,
            },
            {
              x: 114.33,
              y: 135.82,
            },
            {
              x: 105.34,
              y: 144.54,
            },
            {
              x: 84.75,
              y: 141.58,
            },
          ],
          transform: [
            1.44151898, 0.414727913, 0.00487706052, -0.0752875274, 0.244304354,
            1.4780552, 0.407866175, -1.42067454, 0.255595469, 405.509211,
            124.7866, 30.4912472,
          ],
        },
      ],
      extruders: [1, 2, 3, 4],
      painting: {
        multimaterial: true,
        supports: true,
      },
    },
  ],
  thumbnails: [
    {
      index: 0,
      bed: {
        setup: 0,
        bed: 0,
      },
      media_type: 'image/png',
      width: 512,
      height: 512,
    },
    {
      index: 1,
      bed: {
        setup: 0,
        bed: 1,
      },
      media_type: 'image/png',
      width: 512,
      height: 512,
    },
    {
      index: 2,
      bed: {
        setup: 0,
        bed: 2,
      },
      media_type: 'image/png',
      width: 512,
      height: 512,
    },
  ],
  issues: [
    {
      code: 'unsupported_producer',
      detail: 'BambuStudio-01.07.03.50',
    },
  ],
  extruders: [1, 2, 3, 4],
  title: 'Figures',
};

/** PrusaSlicer 2.6 on one extruder: six filament changes up a plaque. */
export const colourChanges: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.6.0+linux-x64-GTK3',
    name: 'PrusaSlicer',
    version: '2.6.0',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Plaque',
  counts: {
    setups: 1,
    beds: 1,
    objects: 1,
    instances: 1,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MINI',
      printer_profile: 'Original Prusa MINI & MINI+',
      print_profile: '0.20mm QUALITY @MINI',
      material_profiles: ['Generic PLA'],
      nozzles: [
        {
          diameter_mm: 0.4,
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
      label_objects: '1',
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: false,
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
            x: 180,
            y: 180,
          },
          instances: [
            {
              object: 0,
              instance: 0,
            },
          ],
          layer_changes: [
            {
              height_mm: 0.6000000238418579,
              kind: 'color_change',
              extruder: 1,
              color: '#626262',
            },
            {
              height_mm: 2.200000047683716,
              kind: 'color_change',
              extruder: 1,
              color: '#FFFFFF',
            },
            {
              height_mm: 2.4000000953674316,
              kind: 'color_change',
              extruder: 1,
              color: '#FF0000',
            },
            {
              height_mm: 2.799999952316284,
              kind: 'color_change',
              extruder: 1,
              color: '#8B6914',
            },
            {
              height_mm: 3.200000047683716,
              kind: 'color_change',
              extruder: 1,
              color: '#434343',
            },
            {
              height_mm: 3.5999999046325684,
              kind: 'color_change',
              extruder: 1,
              color: '#FFFFFF',
            },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'stork_plaque.stl',
      parts: [
        {
          name: 'stork_plaque.stl',
          type: 'model',
          extruder: 1,
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
            x: 129.99241638183594,
            y: 89.9962043762207,
            z: 3.798999309539795,
          },
          min: {
            x: 25.00379180908203,
            y: 43.001895904541016,
          },
          outline: [
            {
              x: 25,
              y: 46.75,
            },
            {
              x: 25.6,
              y: 44.89,
            },
            {
              x: 27.44,
              y: 43.32,
            },
            {
              x: 150.87,
              y: 43,
            },
            {
              x: 153.21,
              y: 43.67,
            },
            {
              x: 154.52,
              y: 45.11,
            },
            {
              x: 155,
              y: 129,
            },
            {
              x: 154.52,
              y: 130.89,
            },
            {
              x: 152.78,
              y: 132.58,
            },
            {
              x: 150.87,
              y: 133,
            },
            {
              x: 27,
              y: 132.46,
            },
            {
              x: 25.6,
              y: 131.11,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 90, 88, 1.89949965],
        },
      ],
      extruders: [1],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [1],
};

/** PrusaSlicer 2.6 on an XL: a temperature tower on the second tool, with custom G-code every 10 mm. */
export const customLayers: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.6.1-rc1+MacOS-x64',
    name: 'PrusaSlicer',
    version: '2.6.1-rc1',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Temperature tower',
  counts: {
    setups: 1,
    beds: 1,
    objects: 1,
    instances: 1,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'XL2',
      printer_profile: 'Original Prusa XL - 2T 0.6 nozzle',
      print_profile: '0.20mm DETAIL @XL 0.6',
      material_profiles: ['Prusament PETG @PG 0.6', 'Prusament PETG @PG 0.6'],
      nozzles: [
        {
          diameter_mm: 0.6,
        },
        {
          diameter_mm: 0.6,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 360,
          y: 0,
        },
        {
          x: 360,
          y: 360,
        },
        {
          x: 0,
          y: 360,
        },
      ],
      max_print_height_mm: 360,
      technology: 'FFF',
      label_objects: '1',
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 20,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PETG',
            colour: '#FF8000',
          },
          {
            type: 'PETG',
            colour: '#DB5182',
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
            x: 360,
            y: 360,
          },
          instances: [
            {
              object: 0,
              instance: 0,
            },
          ],
          layer_changes: [
            {
              height_mm: 10.799999999999995,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S225',
            },
            {
              height_mm: 20.79999999999996,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S230',
            },
            {
              height_mm: 30.799999999999926,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S235',
            },
            {
              height_mm: 40.80000000000005,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S240',
            },
            {
              height_mm: 50.80000000000019,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S245',
            },
            {
              height_mm: 60.80000000000033,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S250',
            },
            {
              height_mm: 70.80000000000047,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S255',
            },
            {
              height_mm: 80.80000000000061,
              kind: 'custom',
              extruder: 1,
              extra: 'M104 S260',
            },
          ],
        },
      ],
    },
  ],
  objects: [
    {
      index: 0,
      name: 'temp-tower-no-numbers.stl',
      parts: [
        {
          name: 'temp-tower-no-numbers.stl',
          type: 'model',
          extruder: 2,
        },
        {
          name: '260',
          type: 'negative_volume',
        },
        {
          name: '255',
          type: 'negative_volume',
        },
        {
          name: '250',
          type: 'negative_volume',
        },
        {
          name: '245',
          type: 'negative_volume',
        },
        {
          name: '240',
          type: 'negative_volume',
        },
        {
          name: '235',
          type: 'negative_volume',
        },
        {
          name: '230',
          type: 'negative_volume',
        },
        {
          name: '225',
          type: 'negative_volume',
        },
        {
          name: '220',
          type: 'negative_volume',
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
            x: 89.29999542236328,
            y: 20.000000000000007,
            z: 91,
          },
          min: {
            x: 29.056248288818352,
            y: 25.264478100000005,
          },
          outline: [
            {
              x: 29.06,
              y: 30.26,
            },
            {
              x: 29.73,
              y: 27.76,
            },
            {
              x: 31.56,
              y: 25.93,
            },
            {
              x: 113.36,
              y: 25.26,
            },
            {
              x: 115.86,
              y: 25.93,
            },
            {
              x: 117.69,
              y: 27.76,
            },
            {
              x: 118.36,
              y: 40.26,
            },
            {
              x: 117.69,
              y: 42.76,
            },
            {
              x: 115.86,
              y: 44.59,
            },
            {
              x: 34.06,
              y: 45.26,
            },
            {
              x: 31.56,
              y: 44.59,
            },
            {
              x: 29.73,
              y: 42.76,
            },
          ],
          transform: [
            -1, 1.2246468e-16, 0, -2.71926215e-32, -2.22044605e-16, 1,
            1.2246468e-16, 1, 2.22044605e-16, 73.706246, 35.2644781, 45.5,
          ],
        },
      ],
      extruders: [2],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [2],
};

/** PrusaSlicer 2.6: four boxes, each with a layer range of its own, and two colour changes. */
export const layerRanges: ProjectDocument = {
  producer: {
    application: 'PrusaSlicer-2.6.1+MacOS-x64',
    name: 'PrusaSlicer',
    version: '2.6.1',
    format_version: '1',
    dialect: 'prusaslicer2',
    supported: true,
  },
  title: 'Side boxes',
  counts: {
    setups: 1,
    beds: 2,
    objects: 4,
    instances: 4,
    non_printable: 2,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'MK4',
      printer_profile: 'Original Prusa MK4 0.4 nozzle',
      print_profile: '0.30mm DRAFT @MK4 0.4',
      material_profiles: ['Prusa PETG @PG'],
      nozzles: [
        {
          diameter_mm: 0.4,
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
      label_objects: '1',
      settings: {
        layer_height_mm: 0.3,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'gyroid',
        perimeters: 2,
        supports: 'everywhere',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PETG',
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
            y: 210,
          },
          instances: [
            {
              object: 2,
              instance: 0,
            },
            {
              object: 3,
              instance: 0,
            },
          ],
          layer_changes: [
            {
              height_mm: 3.200000047683716,
              kind: 'color_change',
              extruder: 1,
              color: '#80EE2E',
            },
            {
              height_mm: 37.099998474121094,
              kind: 'color_change',
              extruder: 1,
              color: '#FD8008',
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
              instance: 0,
            },
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
      name: 'Short Left Side Box with Anchor Hole',
      parts: [
        {
          name: 'Left Side',
          type: 'model',
          extruder: 1,
        },
      ],
      instances: [
        {
          index: 0,
          item: 0,
          printable: false,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 93,
            y: 170,
            z: 40,
          },
          min: {
            x: 29,
            y: 16.949709,
          },
          outline: [
            {
              x: 29,
              y: 26.95,
            },
            {
              x: 29.09,
              y: 25.64,
            },
            {
              x: 29.76,
              y: 23.12,
            },
            {
              x: 31.07,
              y: 20.86,
            },
            {
              x: 32.91,
              y: 19.02,
            },
            {
              x: 35.17,
              y: 17.71,
            },
            {
              x: 37.69,
              y: 17.04,
            },
            {
              x: 122,
              y: 16.95,
            },
            {
              x: 79,
              y: 186.95,
            },
            {
              x: 31.07,
              y: 135.04,
            },
            {
              x: 29.76,
              y: 132.78,
            },
            {
              x: 29.09,
              y: 130.25,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 375.5, 101.949709, 20],
        },
      ],
      extruders: [1],
      layer_ranges: [
        {
          min_z_mm: 3.1,
          max_z_mm: 36.9,
          settings: {
            bottom_solid_layers: '0',
            extruder: '0',
            fill_density: '20%',
            fill_pattern: 'gyroid',
            layer_height: '0.3',
            perimeters: '0',
            top_solid_layers: '0',
          },
        },
      ],
    },
    {
      index: 1,
      name: 'Short Right Side Box with Anchor Hole',
      parts: [
        {
          name: 'Right Side',
          type: 'model',
          extruder: 1,
        },
      ],
      instances: [
        {
          index: 0,
          item: 1,
          printable: false,
          bed: {
            setup: 0,
            bed: 1,
          },
          placement: 'inside',
          size: {
            x: 93,
            y: 170,
            z: 40,
          },
          min: {
            x: 128,
            y: 23.050293999999994,
          },
          outline: [
            {
              x: 128,
              y: 23.05,
            },
            {
              x: 211,
              y: 23.05,
            },
            {
              x: 213.59,
              y: 23.39,
            },
            {
              x: 216,
              y: 24.39,
            },
            {
              x: 218.07,
              y: 25.98,
            },
            {
              x: 219.66,
              y: 28.05,
            },
            {
              x: 220.66,
              y: 30.46,
            },
            {
              x: 221,
              y: 33.05,
            },
            {
              x: 220.91,
              y: 136.36,
            },
            {
              x: 220.24,
              y: 138.88,
            },
            {
              x: 218.93,
              y: 141.14,
            },
            {
              x: 171,
              y: 193.05,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 474.5, 108.050294, 20],
        },
      ],
      extruders: [1],
      layer_ranges: [
        {
          min_z_mm: 3.1,
          max_z_mm: 36.9,
          settings: {
            bottom_solid_layers: '0',
            extruder: '0',
            fill_density: '20%',
            fill_pattern: 'gyroid',
            layer_height: '0.3',
            perimeters: '0',
            top_solid_layers: '0',
          },
        },
      ],
    },
    {
      index: 2,
      name: 'Long Left Side Box with Anchor Hole',
      parts: [
        {
          name: 'Left Side Box',
          type: 'model',
          extruder: 1,
        },
        {
          name: 'Damper Reinforcement',
          type: 'modifier',
        },
      ],
      instances: [
        {
          index: 0,
          item: 2,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 93,
            y: 203,
            z: 40,
          },
          min: {
            x: 28.5,
            y: 3.5,
          },
          outline: [
            {
              x: 28.5,
              y: 13.5,
            },
            {
              x: 28.59,
              y: 12.19,
            },
            {
              x: 29.26,
              y: 9.67,
            },
            {
              x: 30.57,
              y: 7.41,
            },
            {
              x: 32.41,
              y: 5.57,
            },
            {
              x: 34.67,
              y: 4.26,
            },
            {
              x: 37.19,
              y: 3.59,
            },
            {
              x: 121.5,
              y: 3.5,
            },
            {
              x: 78.5,
              y: 206.5,
            },
            {
              x: 30.57,
              y: 154.59,
            },
            {
              x: 29.26,
              y: 152.33,
            },
            {
              x: 28.59,
              y: 149.81,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 75, 105, 20],
        },
      ],
      extruders: [1],
      layer_ranges: [
        {
          min_z_mm: 3.1,
          max_z_mm: 36.9,
          settings: {
            bottom_solid_layers: '0',
            extruder: '0',
            fill_density: '20%',
            fill_pattern: 'gyroid',
            layer_height: '0.3',
            perimeters: '0',
            top_solid_layers: '0',
          },
        },
      ],
    },
    {
      index: 3,
      name: 'Long Right Side Box with Anchor Hole',
      parts: [
        {
          name: 'Right Side Box',
          type: 'model',
          extruder: 1,
        },
        {
          name: 'Damper Reinforcement',
          type: 'modifier',
        },
      ],
      instances: [
        {
          index: 0,
          item: 3,
          printable: true,
          bed: {
            setup: 0,
            bed: 0,
          },
          placement: 'inside',
          size: {
            x: 93,
            y: 203,
            z: 40,
          },
          min: {
            x: 128.5,
            y: 3.5,
          },
          outline: [
            {
              x: 128.5,
              y: 3.5,
            },
            {
              x: 211.5,
              y: 3.5,
            },
            {
              x: 214.09,
              y: 3.84,
            },
            {
              x: 216.5,
              y: 4.84,
            },
            {
              x: 218.57,
              y: 6.43,
            },
            {
              x: 220.16,
              y: 8.5,
            },
            {
              x: 221.16,
              y: 10.91,
            },
            {
              x: 221.5,
              y: 13.5,
            },
            {
              x: 221.41,
              y: 149.81,
            },
            {
              x: 220.74,
              y: 152.33,
            },
            {
              x: 219.43,
              y: 154.59,
            },
            {
              x: 171.5,
              y: 206.5,
            },
          ],
          transform: [1, 0, 0, 0, 1, 0, 0, 0, 1, 175, 105, 20],
        },
      ],
      extruders: [1],
      layer_ranges: [
        {
          min_z_mm: 3.1,
          max_z_mm: 36.9,
          settings: {
            bottom_solid_layers: '0',
            extruder: '0',
            fill_density: '20%',
            fill_pattern: 'gyroid',
            layer_height: '0.3',
            perimeters: '0',
            top_solid_layers: '0',
          },
        },
      ],
    },
  ],
  thumbnails: [
    {
      index: 0,
      media_type: 'image/png',
      width: 256,
      height: 256,
    },
  ],
  issues: [],
  extruders: [1],
};

/** A .gcode.3mf from BambuStudio: G-code for its plate and no object. */
export const gcodeOnly: ProjectDocument = {
  producer: {
    application: 'BambuStudio-01.01.00.14',
    name: 'BambuStudio',
    version: '01.01.00.14',
    format_version: '1',
    dialect: 'orcaslicer',
    supported: false,
  },
  counts: {
    setups: 1,
    beds: 1,
    objects: 0,
    instances: 0,
    non_printable: 0,
    partly_outside: 0,
    outside: 0,
  },
  setups: [
    {
      index: 0,
      printer_model: 'Bambu Lab X1 Carbon',
      printer_profile: 'Bambu Lab X1 Carbon 0.4 nozzle',
      print_profile: '0.20mm Standard @BBL X1C - No Brim, Flush Infill',
      material_profiles: ['Generic PLA'],
      nozzles: [
        {
          diameter_mm: 0.4,
        },
      ],
      bed_shape: [
        {
          x: 0,
          y: 0,
        },
        {
          x: 256,
          y: 0,
        },
        {
          x: 256,
          y: 256,
        },
        {
          x: 0,
          y: 256,
        },
      ],
      max_print_height_mm: 250,
      technology: 'FFF',
      settings: {
        layer_height_mm: 0.2,
        first_layer_height_mm: 0.2,
        infill_percent: 15,
        infill_pattern: 'grid',
        perimeters: 2,
        supports: 'none',
        brim_width_mm: 0,
        wipe_tower: true,
        filaments: [
          {
            type: 'PLA',
            colour: '#00AE42',
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
            x: 256,
            y: 256,
          },
          thumbnail: 0,
          instances: [],
          gcode: 'Metadata/plate_1.gcode',
        },
      ],
    },
  ],
  objects: [],
  thumbnails: [
    {
      index: 0,
      bed: {
        setup: 0,
        bed: 0,
      },
      media_type: 'image/png',
      width: 512,
      height: 512,
    },
  ],
  issues: [
    {
      code: 'unsupported_producer',
      detail: 'BambuStudio-01.01.00.14',
    },
    {
      code: 'gcode_only',
    },
  ],
};

/** The objects of mmuObjects on one bed printed object by object. */
export const sequential: ProjectDocument = {
  ...mmuObjects,
  setups: mmuObjects.setups.map((setup) => ({
    ...setup,
    beds: setup.beds.map((bed) => ({ ...bed, sequential: true })),
  })),
};
