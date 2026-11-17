<!--
  A path the matcher knows and this build answers with no screen yet. It names
  the path, because the person who typed it or followed it is the one who can
  say whether it was wrong.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';

  import type { AppContext } from '../screens';
  import { APP_NAMESPACE } from '../i18n';

  const t = translateFor(APP_NAMESPACE);

  let { ctx }: { ctx: AppContext } = $props();
</script>

<!-- `data-slot` names the screen so the navigation sweep under `test/` can
     fail on it. A person reads the path; a test reads this. -->
<div data-slot="not-found">
  <h1 class="text-xl font-medium">{t('app.title')}</h1>
  <p class="text-muted-foreground">{ctx.path}</p>
</div>
