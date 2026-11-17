// `toast` is svelte-sonner's own raiser, re-exported so a screen reaches it
// through this package rather than depending on the library by name.
export { toast } from 'svelte-sonner';
export { default as Toaster } from './sonner.svelte';
export type { ToasterProps } from './types';
