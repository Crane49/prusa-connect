// The smallest module with a page. `module.test.ts` builds it and reads what
// came out, so this file is what a bundle is measured against.
import { defineModule } from '@prusa/connect-ui/slots';

import Page from './page.svelte';
import './styles.css';

export default defineModule((host) => {
  host.register.page('spools', Page);
});
