<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ModuleSettingsForm from './module-settings-form.svelte';
  import type { ModuleSettingsFormProps } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Extension Detail/Settings',
    component: ModuleSettingsForm,
  });

  const options: ModuleSettingsFormProps['options'] = [
    {
      key: 'label',
      type: 'string',
      summary: 'Shown on the shelf.',
      default: 'Spools',
    },
    {
      key: 'shelf_size',
      type: 'int',
      summary: 'Spools per shelf.',
      required: true,
    },
    { key: 'ratio', type: 'number', summary: 'Fill ratio.' },
    { key: 'endpoint', type: 'url', summary: 'Where the stock lives.' },
    { key: 'enabled', type: 'bool', summary: 'Track spools.' },
    { key: 'token', type: 'secret', summary: 'Access token.' },
    { key: 'tags', type: 'list', summary: 'Tags to follow.' },
    { key: 'limits', type: 'object', summary: 'Limits per material.' },
  ];

  const base: ModuleSettingsFormProps = {
    options,
    config: {
      label: 'Spools',
      shelf_size: 4,
      enabled: true,
      token: 'xxxxx',
      tags: ['pla'],
    },
    saving: false,
    onsave: () => Promise.resolve(true),
    onreload: () => undefined,
  };
</script>

<Story name="Fields" asChild>
  <ModuleSettingsForm {...base} />
</Story>

<Story name="Errors" asChild>
  <ModuleSettingsForm
    {...base}
    config={{
      shelf_size: 'x',
      ratio: 'y',
      endpoint: 'z',
      tags: 'q',
      limits: 'w',
    }}
  />
</Story>

<Story name="Locked" asChild>
  <ModuleSettingsForm
    {...base}
    locked="This extension is set by a file, so it cannot be changed here."
  />
</Story>

<Story name="Saving" asChild>
  <ModuleSettingsForm {...base} saving />
</Story>

<Story name="Conflict" asChild>
  <ModuleSettingsForm {...base} conflict />
</Story>

<Story name="Fault" asChild>
  <ModuleSettingsForm
    {...base}
    fault={new Error('The server refused the settings.')}
  />
</Story>
