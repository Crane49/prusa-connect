<!--
  A settings pane under the page's title. The rail lists every pane, as rows
  when it is open and in a menu when it is collapsed, so the page names none of
  the others.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import { PageHeader } from '../../primitives';
  import type { SettingsScreenProps } from './types';

  let { children }: SettingsScreenProps = $props();
</script>

<div class="flex flex-col gap-4">
  <PageHeader title={t('settings.title')} />

  {@render children?.()}
</div>
