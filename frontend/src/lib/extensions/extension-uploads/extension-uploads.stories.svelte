<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtensionUploads from './extension-uploads.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Uploads',
    component: ExtensionUploads,
  });

  const upload = {
    method: 'PUT',
    path: '/connect/app/teams/{team}/files/raw',
    max_mib: 1024,
  };

  const onsave = () => Promise.resolve(true);
</script>

<Story name="As Approved" asChild>
  <ExtensionUploads
    routes={[upload]}
    asked={[upload]}
    saving={false}
    {onsave}
  />
</Story>

<Story name="Changed By The Operator" asChild>
  <ExtensionUploads
    routes={[{ ...upload, max_mib: 3072 }]}
    asked={[upload]}
    saving={false}
    {onsave}
  />
</Story>

<Story name="Saving" asChild>
  <ExtensionUploads routes={[upload]} asked={[upload]} saving {onsave} />
</Story>

<Story name="Locked" asChild>
  <ExtensionUploads
    routes={[upload]}
    asked={[upload]}
    locked="This extension is declared in the configuration file."
    saving={false}
    {onsave}
  />
</Story>

<Story name="Conflict" asChild>
  <ExtensionUploads
    routes={[upload]}
    asked={[upload]}
    saving={false}
    conflict
    {onsave}
  />
</Story>

<Story name="Refused" asChild>
  <ExtensionUploads
    routes={[upload]}
    asked={[upload]}
    saving={false}
    fault={new Error('the server refused the limit')}
    {onsave}
  />
</Story>
