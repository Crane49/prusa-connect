// What a module author writes, with one difference: the helper is reached by
// a relative path because this fixture lives inside the library it tests. A
// real module writes `from '@prusa/connect-ui/build'`.
import { defineConfig } from 'vite';

import { moduleDev } from '../../dev.ts';
import { moduleBuild } from '../../module.ts';

export default defineConfig(({ command }) =>
  command === 'serve'
    ? moduleDev({ manifest: 'module.yaml' })
    : moduleBuild({ entry: 'src/index.ts' }),
);
