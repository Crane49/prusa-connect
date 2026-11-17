<script lang="ts">
  import type { Snippet } from 'svelte';

  import { FEATURES } from '@prusa/connect-ui/domain';
  import { setReportHref } from '@prusa/connect-ui/primitives';

  import { PATHS } from './routes';

  // The screen belongs to the person signed in. When another person signs in
  // without the page reloading, the screen is torn down and mounted fresh, so
  // nothing it read for the first person is drawn for the second.
  let {
    person,
    diagnostics,
    children,
  }: {
    person: string | undefined;
    /** Whether this server serves the diagnostics bundle. */
    diagnostics: boolean;
    children: Snippet;
  } = $props();

  // Only a signed-in person can open Diagnostics, so a failure drawn before
  // sign-in carries no link to it. A server without the bundle gets no link
  // either. The frame mounts after the settings are read, so the value read
  // here is the server's answer.
  // svelte-ignore state_referenced_locally
  if (FEATURES.diagnostics && diagnostics)
    setReportHref(PATHS.settingsDiagnostics);
</script>

{#key person}
  {@render children()}
{/key}
