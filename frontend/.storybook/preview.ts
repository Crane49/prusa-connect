// What every story shares: the library's theme and skin switches, the English
// locale with the app's own strings registered, the app's stylesheet, and the
// fleet-size switch.
import type { Decorator, Preview } from '@storybook/svelte';

import library from '../../packages/svelte/.storybook/preview';
// After the library's preview, which brings its own Tailwind build: the app's
// sheet comes later in the cascade, so its utilities win where both define a
// class, as `.hidden` against a container-query `block`.
import '../src/styles/global.css';
import { startLocale } from '../src/lib/i18n';
import type { FleetSize } from './fleet';
import FleetFrame from './fleet-frame.svelte';

startLocale(['en']);

const INITIAL_FLEET: FleetSize = 'many';

const SIZES: readonly string[] = ['one', 'two', 'many'] satisfies FleetSize[];

const isSize = (value: unknown): value is FleetSize =>
  typeof value === 'string' && SIZES.includes(value);

const withFleet: Decorator = (_story, context) => {
  const asked: unknown = context.globals.printers;

  return {
    Component: FleetFrame,
    props: { size: isSize(asked) ? asked : INITIAL_FLEET },
  };
};

const preview: Preview = {
  ...library,
  decorators: [...(library.decorators as Decorator[]), withFleet],
  initialGlobals: { ...library.initialGlobals, printers: INITIAL_FLEET },
  globalTypes: {
    ...library.globalTypes,
    // Read by the app screen stories through `AppAt`; a story that does not
    // read it draws the same under every value.
    printers: {
      description: 'Printers in the fleet',
      toolbar: {
        icon: 'grid',
        items: [
          { value: 'one', title: '1 printer' },
          { value: 'two', title: '2 printers' },
          { value: 'many', title: 'Many printers' },
        ],
      },
    },
  },
  parameters: {
    options: {
      storySort: {
        order: ['Tour', 'Screens', 'Parts', 'Shell', 'Primitives'],
      },
    },
  },
};

export default preview;
