import type { Linter } from 'eslint';

export declare const libraryConfig: (options: {
  tsconfigRootDir: string;
  svelteConfig: unknown;
}) => Linter.Config[];

export default libraryConfig;
