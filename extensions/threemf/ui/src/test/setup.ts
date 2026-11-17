// What every test in this package renders against: the theme `theme.css`
// explains, and this module's own catalogue under its namespace, so a text
// reads as a person reads it and never as its key.
import './theme.css';

import { locale, registerCatalogue } from '@prusa/connect-ui/i18n';

import { NAMESPACE } from '../i18n';
import english from '../locales/en.json';

registerCatalogue(NAMESPACE, 'en', english);
locale.set('en');
