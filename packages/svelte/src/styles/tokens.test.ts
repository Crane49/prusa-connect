import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { compile } from '@tailwindcss/node';
import { beforeAll, describe, expect, it } from 'vitest';

// The record of what the theme says. Ported from the React design system's own
// `styles/tokens.test.ts` on the `legacy/main` branch, assertion for
// assertion, because that Storybook is the specification and this theme is a
// copy of its stylesheet.

const stylesDir = dirname(fileURLToPath(import.meta.url));

const css = readFileSync(join(stylesDir, 'tokens.css'), 'utf8');

// tokens.css carries no Tailwind import; that line is the consumer's. The
// compiler needs one, so the compile tests wrap the file the way a consumer
// does, the same way contrast.test.ts beside this file does.
const compilable = `@import 'tailwindcss' source(none);\n${css}`;

// Matches braces so a nested at-rule cannot end the slice early.
const sliceBlock = (marker: string): string => {
  const start = css.indexOf(marker);
  if (start === -1) throw new Error(`marker not found: ${marker}`);
  const open = css.indexOf('{', start);
  if (open === -1) throw new Error(`no block opens after ${marker}`);
  let depth = 0;
  for (let i = open; i < css.length; i += 1) {
    if (css[i] === '{') depth += 1;
    else if (css[i] === '}') {
      depth -= 1;
      if (depth === 0) return css.slice(open + 1, i);
    }
  }
  throw new Error(`unbalanced braces after ${marker}`);
};

const declares = (block: string, token: string, value: string): boolean =>
  new RegExp(
    `^\\s*${token}:\\s*${value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')};\\s*$`,
    'm',
  ).test(block);

const theme = sliceBlock('@theme inline');

/**
 * The skins the stylesheet declares, in source order. The first is the default
 * and carries no `data-skin` attribute; every later one is selected by
 * `:root[data-skin='<name>']`.
 */
const SKINS: string[] = [
  ...css.matchAll(/\/\* @tokens ([a-z]+) light \*\//g),
].map((match) => match[1]!);
const DEFAULT_SKIN = SKINS[0]!;

const THEMES = ['light', 'dark'] as const;
type Theme = (typeof THEMES)[number];

const tokenBlock = (skin: string, mode: Theme): string =>
  sliceBlock(`/* @tokens ${skin} ${mode} */`);

/**
 * The blocks one skin/theme combination resolves a token through, most
 * specific first — the cascade the browser walks, spelled out.
 *
 * `:root[data-skin='…']` scores 0-2-0 and `.dark` 0-1-0, so a non-default
 * skin's light block outranks the default skin's dark block. That is why it
 * sits above it here, and why the specificity guard below insists a skin
 * redeclare in dark whatever it sets in light.
 */
const cascade = (skin: string, mode: Theme): string[] =>
  skin === DEFAULT_SKIN
    ? [tokenBlock(skin, mode), tokenBlock(skin, 'light')]
    : [
        tokenBlock(skin, mode),
        tokenBlock(skin, 'light'),
        tokenBlock(DEFAULT_SKIN, mode),
        tokenBlock(DEFAULT_SKIN, 'light'),
      ];

/**
 * What each skin pins, per theme: `[token, light, dark]`, with `null` where the
 * skin inherits the default skin's value rather than declaring its own.
 */
const CONTRACT: Record<string, [string, string | null, string | null][]> = {
  prusa: [
    ['--background', '#fff', '#000'],
    ['--foreground', 'var(--color-gray-light-12)', 'var(--color-gray-dark-12)'],
    ['--card', '#fff', 'var(--color-gray-dark-2)'],
    [
      '--card-foreground',
      'var(--color-gray-light-12)',
      'var(--color-gray-dark-12)',
    ],
    ['--popover', '#fff', 'var(--color-gray-dark-2)'],
    [
      '--popover-foreground',
      'var(--color-gray-light-12)',
      'var(--color-gray-dark-12)',
    ],
    ['--primary', 'var(--color-orange-light-9)', 'var(--color-orange-dark-9)'],
    // The Prusa Design System's --color-semantic-primary-*: white on the
    // brand orange, hover a step darker, press a step darker again.
    [
      '--primary-foreground',
      'var(--color-orange-light-1)',
      'var(--color-gray-dark-12)',
    ],
    [
      '--primary-hovered',
      'var(--color-orange-light-10)',
      'var(--color-orange-dark-10)',
    ],
    [
      '--primary-pressed',
      'var(--color-orange-light-11)',
      'var(--color-orange-dark-8)',
    ],
    ['--muted', 'var(--color-gray-light-2)', 'var(--color-gray-dark-2)'],
    [
      '--muted-foreground',
      'var(--color-gray-light-11)',
      'var(--color-gray-dark-11)',
    ],
    ['--accent', 'var(--color-gray-light-3)', 'var(--color-gray-dark-3)'],
    ['--border', 'var(--color-gray-light-5)', 'var(--color-gray-dark-5)'],
    // Where the person currently is. Tracks --primary here: this skin has
    // nothing that says "you are here" differently from "this is the brand".
    ['--selected', 'var(--color-orange-light-9)', 'var(--color-orange-dark-9)'],
    [
      '--selected-foreground',
      'var(--color-orange-light-1)',
      'var(--color-gray-dark-12)',
    ],
    [
      '--selected-hovered',
      'var(--color-orange-light-10)',
      'var(--color-orange-dark-10)',
    ],
    // What a checkbox, a radio or a switch shows once it is on. Tracks
    // --primary for the same reason --selected does.
    [
      '--control-on',
      'var(--color-orange-light-9)',
      'var(--color-orange-dark-9)',
    ],
    [
      '--control-on-foreground',
      'var(--color-orange-light-1)',
      'var(--color-gray-dark-12)',
    ],
    // Work advancing on its own.
    ['--progress', 'var(--color-orange-light-9)', 'var(--color-orange-dark-9)'],
    // Rejected red-light-9: 3.91:1/3.84:1, under 4.5:1. red-light-11 clears it at 5.21:1/5.11:1. Dark keeps red-dark-9, already 5.37:1.
    ['--destructive', 'var(--color-red-light-11)', 'var(--color-red-dark-9)'],
    // A tinted plane for a toned notice, one per tone, at step 3 of each scale.
    [
      '--destructive-surface',
      'var(--color-red-light-3)',
      'var(--color-red-dark-3)',
    ],
    // red-light-1/red-dark-1 clear --destructive at 5.11:1/4.83:1; #fff only reached 3.91:1.
    [
      '--destructive-foreground',
      'var(--color-red-light-1)',
      'var(--color-red-dark-1)',
    ],
    // Rejected red-light-9/red-dark-10: 3.84:1/4.07:1, under 4.5:1. red-light-10/red-dark-11 clear it at 5.21:1/8.60:1.
    [
      '--destructive-hovered',
      'var(--color-red-light-10)',
      'var(--color-red-dark-11)',
    ],
    // The press step, a third fill for a tablet with no hover. Light lightens
    // (step 12 clears --destructive-foreground at 8.43:1's kin); dark runs
    // away from the near-black foreground rather than toward it, 13.83:1.
    [
      '--destructive-pressed',
      'var(--color-red-light-12)',
      'var(--color-red-dark-12)',
    ],
    // Light reuses --destructive (5.21:1). Dark's red-dark-9 only reaches 4.48:1 against --card/--popover; red-dark-11 clears all three at 9.55:1/7.97:1.
    [
      '--destructive-text',
      'var(--color-red-light-11)',
      'var(--color-red-dark-11)',
    ],
    // Light rejected translucent orange-alpha-light-8: composited over #fff, 2.66:1, under WCAG 1.4.11's 3:1. orange-light-9 clears at 3.33:1. Dark keeps orange-alpha-dark-8 (5.93:1/7.09:1).
    [
      '--ring',
      'var(--color-orange-light-9)',
      'var(--color-orange-alpha-dark-8)',
    ],
    ['--disabled', 'var(--color-gray-light-1)', 'var(--color-gray-dark-1)'],
    [
      '--disabled-foreground',
      'var(--color-gray-light-8)',
      'var(--color-gray-dark-8)',
    ],
    ['--success', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
    // The tinted plane a success notice sits on, step 3 like every other tone below.
    [
      '--success-surface',
      'var(--color-green-light-3)',
      'var(--color-green-dark-3)',
    ],
    ['--success-foreground', '#000', '#000'],
    [
      '--success-text',
      'var(--color-green-light-11)',
      'var(--color-green-dark-11)',
    ],
    ['--warning', 'var(--color-yellow-light-9)', 'var(--color-yellow-dark-9)'],
    [
      '--warning-surface',
      'var(--color-yellow-light-3)',
      'var(--color-yellow-dark-3)',
    ],
    ['--warning-foreground', '#000', '#000'],
    [
      '--warning-text',
      'var(--color-yellow-light-12)',
      'var(--color-yellow-dark-11)',
    ],
    // No `--surface-foreground` — `--foreground`/`--muted-foreground` land on it directly, measured in TEXT_PAIRS below.
    ['--surface', 'var(--color-gray-light-3)', 'var(--color-gray-dark-3)'],
    ['--info', 'var(--color-blue-light-9)', 'var(--color-blue-dark-9)'],
    ['--info-surface', 'var(--color-blue-light-3)', 'var(--color-blue-dark-3)'],
    ['--info-foreground', '#000', '#000'],
    ['--info-text', 'var(--color-blue-light-11)', 'var(--color-blue-dark-11)'],
    // Connect's own printer-lifecycle palette, a fill and an edge per literal
    // state rather than the six families folded onto twelve states. Neither
    // skin overrides these, so they live only in this table.
    ['--state-unknown', '#d3d7de', '#424549'],
    ['--state-unknown-edge', '#9daabd', '#9daabd'],
    ['--state-offline', '#d8d8d8', '#454545'],
    ['--state-offline-edge', '#adadad', '#adadad'],
    ['--state-ready', '#cbdfc9', '#3f4a3e'],
    ['--state-ready-edge', '#92c78c', '#92c78c'],
    ['--state-busy', '#e6d5ca', '#4e443d'],
    ['--state-busy-edge', '#d5a585', '#d5a585'],
    ['--state-printing', '#c5d4e7', '#3b444e'],
    ['--state-printing-edge', '#7da7d9', '#7da7d9'],
    ['--state-paused', '#e0d0e0', '#4a404a'],
    ['--state-paused-edge', '#c396c4', '#c396c4'],
    ['--state-stopped', '#f2c4ba', '#543a35'],
    ['--state-stopped-edge', '#f8795f', '#f8795f'],
    ['--state-finished', '#d4eec3', '#435238'],
    ['--state-finished-edge', '#a1ea70', '#a1ea70'],
    ['--state-error', '#e9c5c6', '#4f3b3b'],
    ['--state-error-edge', '#dd7b7d', '#dd7b7d'],
    // No colour of its own in Connect's table; takes --state-busy's, being the same kind of claim.
    ['--state-moving', '#e6d5ca', '#4e443d'],
    ['--state-moving-edge', '#d5a585', '#d5a585'],
  ],
  // Only what this skin changes. Everything absent resolves to the row above,
  // which is what keeps the two palettes one design rather than two.
  slicer: [
    ['--background', 'var(--color-gray-light-2)', '#0f0f0f'],
    ['--foreground', null, '#fff'],
    ['--card', null, '#2a2a2a'],
    ['--card-foreground', null, '#fff'],
    ['--popover', null, '#2a2a2a'],
    ['--popover-foreground', null, '#fff'],
    ['--primary', null, '#fa6831'],
    // The reference's own white on its orange, as the design system does.
    ['--primary-foreground', null, '#fff'],
    ['--primary-hovered', null, '#ff7b4a'],
    ['--primary-pressed', null, '#e05a26'],
    ['--secondary', '#fff', '#2a2a2a'],
    ['--secondary-foreground', 'var(--color-gray-light-12)', '#fff'],
    ['--secondary-hovered', 'var(--color-gray-light-3)', '#323232'],
    ['--muted', 'var(--color-gray-light-3)', '#1b1b1b'],
    ['--muted-foreground', null, '#b8b8b8'],
    ['--accent', null, '#323232'],
    ['--surface', null, '#323232'],
    ['--border', 'var(--color-gray-light-6)', '#3c3c3c'],
    ['--ring', null, '#fa6831'],
    ['--disabled', null, '#1b1b1b'],
    ['--disabled-foreground', null, '#808080'],
    // PrusaSlicer paints its current tab and rail item this navy rather than
    // the brand's orange; declared in dark only, the reference has no light
    // theme to read a light value off, so light stays on the default skin.
    ['--selected', null, '#364976'],
    ['--selected-foreground', null, '#fff'],
    ['--selected-hovered', null, '#425890'],
    ['--control-on', null, '#4e81f8'],
    ['--control-on-foreground', null, '#0f0f0f'],
    ['--progress', null, '#7397ec'],
  ],
  // Green marks what can be acted on; every surface, hairline and neutral
  // inherits. The green fills and the text on them are the design system's
  // Connect theme, step for step.
  prusapro: [
    ['--primary', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
    [
      '--primary-foreground',
      'var(--color-green-light-1)',
      'var(--color-gray-dark-12)',
    ],
    [
      '--primary-hovered',
      'var(--color-green-light-10)',
      'var(--color-green-dark-10)',
    ],
    [
      '--primary-pressed',
      'var(--color-green-light-11)',
      'var(--color-green-dark-8)',
    ],
    // Follows the brand here, as it does in the default skin: green is what
    // can be acted on, and where you are is part of that.
    ['--selected', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
    [
      '--selected-foreground',
      'var(--color-green-light-1)',
      'var(--color-gray-dark-12)',
    ],
    [
      '--selected-hovered',
      'var(--color-green-light-10)',
      'var(--color-green-dark-10)',
    ],
    ['--control-on', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
    [
      '--control-on-foreground',
      'var(--color-green-light-1)',
      'var(--color-gray-dark-12)',
    ],
    ['--progress', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
    ['--secondary', 'var(--color-green-light-5)', 'var(--color-green-dark-5)'],
    [
      '--secondary-foreground',
      'var(--color-green-light-12)',
      'var(--color-green-dark-12)',
    ],
    [
      '--secondary-hovered',
      'var(--color-green-light-7)',
      'var(--color-green-dark-7)',
    ],
    // A step off --primary, so a success badge cannot be mistaken for the
    // brand button. Dark takes step 11, not step 8: #000 measures 4.17:1 on
    // green-dark-8, under AA.
    ['--success', 'var(--color-green-light-8)', 'var(--color-green-dark-11)'],
    ['--ring', 'var(--color-green-light-9)', 'var(--color-green-dark-9)'],
  ],
};

/** Every token name any skin pins — what @theme inline has to register. */
const REGISTERED = [
  ...new Set(
    Object.values(CONTRACT)
      .flat()
      .map(([token]) => token),
  ),
];

/**
 * The largest corner radius a skin may use. The prusa skin's 4px is a rule of
 * this tree; the slicer skin rounds a tab or a panel at the 5px its reference
 * does, so the cap is per skin rather than one number for the stylesheet.
 */
const RADIUS_CAP: Record<string, number> = {
  prusa: 4,
  slicer: 5,
  prusapro: 4,
};

const declaredTokens = (block: string): string[] =>
  [...block.matchAll(/^\s*(--[a-z0-9-]+):/gm)].map((match) => match[1]!);

describe('token contract', () => {
  it('pins a table for every skin the stylesheet declares', () => {
    expect([...Object.keys(CONTRACT)].sort()).toEqual([...SKINS].sort());
  });

  describe.each(SKINS)('%s', (skin) => {
    it.each(CONTRACT[skin]!)(
      '%s carries the value this skin pins',
      (token, l, d) => {
        if (l !== null)
          expect(declares(tokenBlock(skin, 'light'), token, l)).toBe(true);
        if (d !== null)
          expect(declares(tokenBlock(skin, 'dark'), token, d)).toBe(true);
      },
    );
  });

  // Undeclared in @theme inline emits no Tailwind utility, and nothing else in the toolchain reports it.
  it.each(REGISTERED)('%s is registered as a Tailwind colour', (token) => {
    const name = token.replace(/^--/, '');
    expect(
      new RegExp(`^\\s*--color-${name}:\\s*var\\(${token}\\);\\s*$`, 'm').test(
        theme,
      ),
    ).toBe(true);
  });
});

// A skin's own block outscores `.dark`, so a token it sets for light keeps
// that value in the dark theme unless the skin's dark block sets it again —
// silently, since every name still resolves to something.
describe.each(SKINS.filter((skin) => skin !== DEFAULT_SKIN))(
  '%s skin specificity',
  (skin) => {
    it('redeclares in dark every token its light block sets', () => {
      const inDark = new Set(declaredTokens(tokenBlock(skin, 'dark')));
      expect(
        declaredTokens(tokenBlock(skin, 'light')).filter(
          (token) => !inDark.has(token),
        ),
      ).toEqual([]);
    });
  },
);

describe('raw palette', () => {
  // Guards against a short/missing palette copy, which would let every contract assertion above pass vacuously.
  it.each([
    '--color-gray-light-12',
    '--color-gray-dark-12',
    '--color-gray-dark-2',
    '--color-orange-light-9',
    '--color-orange-dark-9',
    '--color-orange-alpha-light-8',
    '--color-red-light-9',
    '--color-green-dark-9',
    '--color-yellow-dark-9',
    '--color-blue-light-11',
  ])('%s is declared', (token) => {
    expect(new RegExp(`^\\s*${token}:\\s*\\S`, 'm').test(css)).toBe(true);
  });

  it('carries the full 12-step gray ramp in both modes', () => {
    for (const mode of ['light', 'dark']) {
      for (let step = 1; step <= 12; step += 1) {
        expect(
          new RegExp(
            `^\\s*--color-gray-${mode}-${String(step)}:\\s*\\S`,
            'm',
          ).test(css),
        ).toBe(true);
      }
    }
  });
});

// A new --animate-* token added without a matching `none` line in the reduced-motion `@media` block animates for everybody, silently.
// --duration-progress is checked by name since it isn't an --animate-* token.
describe('reduced motion switches off every motion token', () => {
  const motion = sliceBlock('/* ── Motion');
  const reduced = sliceBlock('@media (prefers-reduced-motion: reduce)');
  const declared: string[] = [
    ...motion.matchAll(/^\s*(--animate-[a-z0-9-]+):/gm),
  ].map((match) => match[1]!);

  it('declares animation tokens to check', () => {
    expect(declared.length).toBeGreaterThan(12);
  });

  it.each(declared)('%s is none under reduced motion', (token) => {
    expect(new RegExp(`^\\s*${token}:\\s*none;`, 'm').test(reduced)).toBe(true);
  });

  it('takes the progress arc\u2019s travel to zero', () => {
    expect(/--duration-progress:\s*0s;/.test(reduced)).toBe(true);
  });

  it('stops the focus lift from rising', () => {
    expect(/--focus-lift-travel:\s*0px;/.test(reduced)).toBe(true);
  });
});

describe('visual language', () => {
  it('registers a monospace font family', () => {
    expect(theme.includes('--font-mono:')).toBe(true);
  });

  it('registers a heading font family', () => {
    expect(theme.includes('--font-heading:')).toBe(true);
  });

  it('lifts a focused control differently in light and dark', () => {
    const light = tokenBlock(DEFAULT_SKIN, 'light');
    const dark = tokenBlock(DEFAULT_SKIN, 'dark');
    // The shadow and the lit edge swap roles between themes; these substrings
    // are what tells the two apart rather than the exact wrapping of the value.
    expect(light).toContain('rgb(255 255 255 / 0.55)');
    expect(dark).toContain('rgb(255 255 255 / 0.28)');
  });

  // Radius and the type/spacing rhythm are the two non-colour things a skin
  // swaps. Both reach Tailwind through @theme inline, so a literal written
  // back here would freeze every skin at one value with nothing failing.
  it('routes every radius step through the skin', () => {
    for (const step of ['xs', 'sm', 'md']) {
      expect(theme).toMatch(
        new RegExp(`^\\s*--radius-${step}: var\\(--corner-${step}\\);$`, 'm'),
      );
    }
  });

  it('scales the type steps and the spacing unit by the skin’s density', () => {
    const scaled = [
      ...theme.matchAll(/--(?:text-[a-z0-9]+|spacing):\s*([^;]+);/g),
    ].map((match) => match[1]);
    expect(scaled.length).toBeGreaterThan(5);
    for (const value of scaled) expect(value).toContain('var(--density)');
  });

  describe.each(SKINS)('%s', (skin) => {
    const shape = sliceBlock(`/* @shape ${skin} */`);

    it('stays within its own corner cap', () => {
      const radii = [...shape.matchAll(/--corner-[a-z0-9]+:\s*(\d+)px;/g)].map(
        (match) => Number(match[1]),
      );
      expect(radii.length).toBe(3);
      expect(Math.max(...radii)).toBeLessThanOrEqual(RADIUS_CAP[skin]!);
    });

    it('declares a density', () => {
      const density = /--density:\s*([\d.]+);/.exec(shape);
      expect(density).not.toBeNull();
      expect(Number(density![1])).toBeGreaterThan(0);
    });
  });

  it('leaves the default skin at density 1', () => {
    expect(sliceBlock(`/* @shape ${DEFAULT_SKIN} */`)).toMatch(
      /--density:\s*1;/,
    );
  });

  it('carries no legacy or bridge vocabulary', () => {
    expect(css).not.toMatch(/--legacy-/);
    expect(css).not.toMatch(/--ds-/);
  });
});

// Runs the real compiler: a source-text check can't see Tailwind's built-in default theme (a full palette, a radius scale to .75rem) unless reset.
describe('tailwind default theme reset', () => {
  let build: (candidates: string[]) => string;

  beforeAll(async () => {
    const compiled = await compile(compilable, {
      base: stylesDir,
      onDependency: () => undefined,
    });
    build = (candidates) => compiled.build(candidates);
  });

  it('emits nothing for stock-palette or oversized-radius utilities', () => {
    // None of these classes exist in src/next; they stand in for a future primitive copied from a component still carrying upstream's rounded-lg or an arbitrary Tailwind colour.
    // The reset has two deliberate exceptions, --color-white/--color-black, covered by keyword-colours.test.ts instead.
    const out = build([
      'bg-red-500',
      'text-blue-400',
      'rounded-lg',
      'rounded-xl',
    ]);
    expect(out).not.toMatch(/--color-red-500/);
    expect(out).not.toMatch(/--color-blue-400/);
    expect(out).not.toMatch(/--radius-lg/);
    expect(out).not.toMatch(/--radius-xl/);
  });

  // `p-5` rather than `p-6`: block 4 still pins Tailwind's own low steps onto
  // its nine-step scale, and a step it does not name is the one that shows the
  // skin's density reaching the spacing unit.
  it('still resolves this project’s own tokens through the same compiler', () => {
    const out = build(['bg-primary', 'rounded-sm', 'p-5']);
    expect(out).toContain('.bg-primary');
    // Radius and spacing reach the rule as the skin's own variables, which is
    // what lets `data-skin` change them without a second utility.
    expect(
      /\.rounded-sm\s*\{\s*border-radius:\s*var\(--corner-sm\);?\s*\}/.test(
        out,
      ),
    ).toBe(true);
    expect(out).toContain('var(--density)');
  });

  // Guards the class strings the rail and the phone bar already write: absent
  // from @theme inline, each of these compiles to nothing and leaves the
  // current row with no fill at all.
  it('compiles the selected and control-on utilities', () => {
    const out = build([
      'bg-selected',
      'border-selected',
      'text-selected-foreground',
      'hover:bg-selected-hovered',
      'bg-control-on',
    ]);
    expect(out).toContain('.bg-selected');
    expect(out).toContain('.border-selected');
    expect(out).toContain('.text-selected-foreground');
    expect(out).toContain('bg-selected-hovered');
    expect(out).toContain('.bg-control-on');
  });
});

// @source's effect is which files Tailwind's scanner reads, invisible to a source-text check.
describe('tailwind source scoping', () => {
  it('disables automatic project-wide scanning', async () => {
    const { root } = await compile(compilable, {
      base: stylesDir,
      onDependency: () => undefined,
    });
    expect(root).toBe('none');
  });

  // A module imports this file for the theme and the `dark` variant, and emits
  // only the utilities it names itself (ADR-20260914-244 rules 10 to 13). A scan here
  // would put the whole library's in every module bundle.
  it('names no source of its own', async () => {
    const { sources } = await compile(compilable, {
      base: stylesDir,
      onDependency: () => undefined,
    });
    expect(sources).toEqual([]);
  });

  // app.css is the other half: the file an application imports, carrying the
  // scan that tokens.css must not. `@source` takes a path and never a package
  // name, so this is the only place the path can be written safely, with both
  // ends inside this package.
  it('names this package as the one source an application scans', async () => {
    const appCss = readFileSync(join(stylesDir, 'app.css'), 'utf8');
    const { sources } = await compile(
      `@import 'tailwindcss' source(none);\n${appCss}`,
      { base: stylesDir, onDependency: () => undefined },
    );

    expect(sources).toHaveLength(1);
    expect(sources[0]?.base).toBe(stylesDir);
    expect(sources[0]?.pattern).toBe('../');
  });
});

// Ratios cited in comments above were computed by hand and can't recompute themselves, so this suite re-measures every pair that meets on screen.
// OKLCH → sRGB (CSS Color 4) then WCAG 2.1 luminance/contrast, implemented rather than imported for forty lines of arithmetic that won't change.
type Srgb = readonly [number, number, number];

const encode = (v: number): number => {
  const x = Math.min(1, Math.max(0, v));
  return x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055;
};

const oklchToSrgb = (L: number, C: number, hDeg: number): Srgb => {
  const h = (hDeg * Math.PI) / 180;
  const a = C * Math.cos(h);
  const bb = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * bb) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * bb) ** 3;
  const sc = (L - 0.0894841775 * a - 1.291485548 * bb) ** 3;
  return [
    encode(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * sc),
    encode(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * sc),
    encode(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * sc),
  ];
};

const decode = (c: number): number =>
  c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;

const relativeLuminance = ([r, g, b]: Srgb): number =>
  0.2126 * decode(r) + 0.7152 * decode(g) + 0.0722 * decode(b);

const contrast = (fg: Srgb, bg: Srgb): number => {
  const a = relativeLuminance(fg);
  const b = relativeLuminance(bg);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
};

// Two notations appear here: `oklch()` for most of the palette, hex for carried-over brand values and `#fff`/`#000`.
// Anything else returns null so a caller fails loudly instead of measuring a guess.
const parseColour = (value: string): Srgb | null => {
  const oklch = /^oklch\(([\d.]+)%\s+([\d.]+)\s+([\d.]+)\)$/.exec(value);
  if (oklch) {
    return oklchToSrgb(
      Number(oklch[1]) / 100,
      Number(oklch[2]),
      Number(oklch[3]),
    );
  }
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(value);
  if (!hex) return null;
  const digits = hex[1]!;
  const pairs =
    digits.length === 3
      ? (digits.match(/./g) ?? []).map((digit) => digit + digit)
      : [digits.slice(0, 2), digits.slice(2, 4), digits.slice(4, 6)];
  const channel = (pair: string): number => parseInt(pair, 16) / 255;
  return [channel(pairs[0]!), channel(pairs[1]!), channel(pairs[2]!)];
};

/** The raw palette, declared once in `:root` above the two theme blocks. */
const palette = new Map<string, Srgb>();
for (const match of css.matchAll(/(--color-[a-z0-9-]+):\s*([^;]+);/g)) {
  const srgb = parseColour(match[2]!.trim());
  if (srgb) palette.set(match[1]!, srgb);
}

// Throws rather than returning a default: an unresolved token would otherwise be silently skipped by the assertions below.
const resolveToken = (blocks: string[], token: string): Srgb => {
  const match = blocks
    .map((block) => new RegExp(`^\\s*${token}:\\s*([^;]+);`, 'm').exec(block))
    .find((hit) => hit !== null);
  if (!match)
    throw new Error(`${token} is not declared anywhere in this skin's cascade`);
  const value = match[1]!.trim();
  const reference = /^var\((--color-[a-z0-9-]+)\)$/.exec(value);
  if (reference) {
    const named = reference[1]!;
    const srgb = palette.get(named);
    if (!srgb)
      throw new Error(
        `${token} references ${named}, which the palette does not declare in a notation this test parses`,
      );
    return srgb;
  }
  const literal = parseColour(value);
  if (!literal)
    throw new Error(
      `${token} is \`${value}\`, which this test cannot parse as a colour`,
    );
  return literal;
};

// Pairs that meet on screen; AA's 4.5:1 bar for normal text (each carries `text-sm`, 14px, somewhere in `components/ui`).
// `--disabled`/`--disabled-foreground` is absent: WCAG exempts inactive controls.
const TEXT_PAIRS: [fg: string, bg: string][] = [
  ['--foreground', '--background'],
  ['--card-foreground', '--card'],
  ['--popover-foreground', '--popover'],
  // No brand fill here: --primary, --selected and --control-on carry the
  // design system's white, which sits under 4.5:1. contrast.test.ts holds each
  // of those pairs to the ratio it measures.
  ['--secondary-foreground', '--secondary'],
  ['--secondary-foreground', '--secondary-hovered'],
  ['--muted-foreground', '--muted'],
  ['--muted-foreground', '--background'],
  // Not implied by the --background/--muted rows above: --surface is a step away from both. Measured 14.44:1/13.59:1 and 5.22:1/7.60:1.
  ['--foreground', '--surface'],
  ['--muted-foreground', '--surface'],
  ['--destructive-foreground', '--destructive'],
  ['--destructive-foreground', '--destructive-hovered'],
  ['--destructive-text', '--background'],
  // Not implied by the --background pair: --card/--popover sit one step lighter than --background in dark, the gap the prior token fell into at 4.48:1.
  ['--destructive-text', '--card'],
  ['--destructive-text', '--popover'],
  ['--success-foreground', '--success'],
  ['--success-text', '--background'],
  ['--warning-foreground', '--warning'],
  ['--warning-text', '--background'],
  ['--info-foreground', '--info'],
  ['--info-text', '--background'],
];

// Every skin x theme, not just the default pair: a second palette is only
// safe to ship because the same sweep ran over it, and a skin that inherits a
// token still gets measured against whatever surface it *did* change.
describe('text contrast clears WCAG AA in every skin and theme', () => {
  it('declares a palette to measure against', () => {
    // An empty palette would make every assertion below pass vacuously.
    expect(palette.size).toBeGreaterThan(40);
  });

  for (const skin of SKINS) {
    for (const mode of THEMES) {
      describe(`${skin} / ${mode}`, () => {
        for (const [fg, bg] of TEXT_PAIRS) {
          it(`${fg} on ${bg}`, () => {
            const blocks = cascade(skin, mode);
            const ratio = contrast(
              resolveToken(blocks, fg),
              resolveToken(blocks, bg),
            );
            expect(
              Number(ratio.toFixed(2)),
              `${fg} on ${bg} in the ${skin} skin's ${mode} theme`,
            ).toBeGreaterThanOrEqual(4.5);
          });
        }
      });
    }
  }
});
