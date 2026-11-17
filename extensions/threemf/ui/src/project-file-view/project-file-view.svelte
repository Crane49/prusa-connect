<!--
  What this module draws in the `file-view` slot of a .3mf file's page
  (ADR-20261027-wvj rule 16). The page hands the file as `<storage>:<path>`.
-->
<script lang="ts">
  import FileXIcon from '@lucide/svelte/icons/file-x';
  import { Card, CardContent, EmptyState } from '@prusa/connect-ui/primitives';
  import { parseFileRef } from '@prusa/connect-ui/slots';

  import { t } from '../i18n';
  import { ProjectFile } from '../project-file';

  interface Props {
    /** The file, as `<storage>:<path>`. */
    fileRef: string;
  }

  let { fileRef }: Props = $props();

  const file = $derived(parseFileRef(fileRef));
</script>

{#if file === undefined}
  <Card>
    <CardContent>
      <EmptyState
        icon={FileXIcon}
        heading={t('projectView.failed.heading')}
        headingLevel="h2"
        description={t('projectView.failed.reference')}
      />
    </CardContent>
  </Card>
{:else}
  <!-- The page's header shows the file's picture, so the overview leaves
       it out (ADR-20261027-4tq rule 19). -->
  <ProjectFile storage={file.storage} path={file.path} picture={false} />
{/if}
