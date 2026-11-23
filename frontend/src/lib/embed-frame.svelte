<!--
  The frame of a page with no app shell around it: an embedded module page
  (`?embed=1`) and the page PrusaSlicer shows while it signs the person in.
  It sets the theme on the document, which the app shell does on every other
  page, and keeps the toasts a module page raises.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import { applyTheme, theme } from '@prusa/connect-ui';
  import { Toaster } from '@prusa/connect-ui/primitives';

  let { children }: { children: Snippet } = $props();

  $effect(() => {
    applyTheme(theme.dark);
  });
</script>

<div
  data-part="embed-frame"
  class="@container/app-shell flex min-h-svh flex-col bg-background text-foreground"
>
  <main class="isolate min-w-0 flex-1 px-4 py-6 @xl/app-shell:px-6">
    {@render children()}
  </main>
  <Toaster />
</div>
