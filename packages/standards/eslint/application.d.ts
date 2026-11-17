import type { Linter } from 'eslint';

export declare const applicationConfig: (options: {
  tsconfigRootDir: string;
  svelteConfig: unknown;
}) => Linter.Config[];

export default applicationConfig;
