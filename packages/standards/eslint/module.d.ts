import type { Linter } from 'eslint';

export declare const moduleConfig: (options: {
  tsconfigRootDir: string;
  svelteConfig: unknown;
}) => Linter.Config[];

export default moduleConfig;
