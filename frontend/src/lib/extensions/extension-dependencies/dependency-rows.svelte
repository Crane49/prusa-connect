<!--
  The modules an extension cannot run without. Each line names the module,
  the constraint on its releases, and how this server meets it.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { Badge } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import {
    type ModuleRequirement,
    shortNameOf,
    standingOf,
  } from '../dependencies';

  let {
    rows,
  }: {
    rows: readonly ModuleRequirement[];
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  const VARIANT = {
    met: 'secondary',
    missing: 'warning',
    off: 'warning',
    outOfRange: 'warning',
  } as const;
</script>

<div class="flex flex-col gap-1" data-part="dependencies-requires">
  <p class="font-medium">
    {t('app.settings.extensions.dependencies.requires')}
  </p>
  <ul class="flex flex-col gap-2">
    {#each rows as row (row.module)}
      {@const standing = standingOf(row)}
      <li class="flex flex-col gap-0.5" data-part="dependency">
        <span class="flex flex-wrap items-center gap-2">
          <span class="font-medium">{shortNameOf(row.module ?? '')}</span>
          <code class="text-xs text-muted-foreground">{row.version}</code>
          <Badge variant={VARIANT[standing]} data-part="dependency-standing">
            {t(`app.settings.extensions.dependencies.standing.${standing}`, {
              installed: row.installed ?? '',
            })}
          </Badge>
        </span>
        <code class="text-xs text-muted-foreground">{row.module}</code>
      </li>
    {/each}
  </ul>
</div>
