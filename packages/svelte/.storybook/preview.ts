// What every story in this Storybook shares: the theme, the two toolbar
// switches and the language.
import './preview.css';

import type { Decorator, Preview } from '@storybook/svelte';

import { locale } from '../src/i18n';
import { DEFAULT_SKIN } from '../src/styles/skin';
import StoryFrame from './story-frame.svelte';

// Every text on a screen is `t('key')` (frontend-i18n.md), and the store
// answers English until something sets another language. Saying so here means
// a story never draws a bare key, whatever the machine's own language is.
locale.set('en');

const withTheme: Decorator = (_story, context) => {
  // Storybook types both maps as open records, so they are read through a
  // narrower type rather than one value at a time.
  const globals: Record<string, string | undefined> = context.globals;

  return {
    Component: StoryFrame,
    props: {
      dark: globals.theme === 'dark',
      skin: globals.skin ?? DEFAULT_SKIN,
    },
  };
};

const preview: Preview = {
  decorators: [withTheme],
  initialGlobals: { theme: 'light', skin: DEFAULT_SKIN },
  globalTypes: {
    theme: {
      description: 'Colour theme',
      toolbar: {
        icon: 'circlehollow',
        items: [
          { value: 'light', title: 'Light' },
          { value: 'dark', title: 'Dark' },
        ],
      },
    },
    skin: {
      description: 'Brand skin',
      toolbar: {
        icon: 'paintbrush',
        items: [
          { value: 'prusa', title: 'Prusa' },
          { value: 'slicer', title: 'PrusaSlicer' },
          { value: 'prusapro', title: 'Prusa PRO' },
        ],
      },
    },
  },
  parameters: {
    // Every story is a primitive, so the sort is the default one: alphabetical
    // inside the one group.
    options: { storySort: { order: ['Primitives'] } },
  },
};

export default preview;
