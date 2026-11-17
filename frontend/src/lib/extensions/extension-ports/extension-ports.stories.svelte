<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtensionPorts from './extension-ports.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Ports',
    component: ExtensionPorts,
  });

  const web = {
    name: 'web',
    network: 'tcp' as const,
    port: 8080,
    why: 'serves its own page',
  };
  const syslog = {
    name: 'syslog',
    network: 'udp' as const,
    port: 5514,
    why: 'receives log lines from printers',
  };

  const onsave = () => Promise.resolve(true);
</script>

<Story name="Not Allowed Yet" asChild>
  <ExtensionPorts asked={[web, syslog]} listening={[]} busy={false} {onsave} />
</Story>

<Story name="Allowed" asChild>
  <ExtensionPorts
    asked={[web, syslog]}
    listening={[web, syslog]}
    busy={false}
    {onsave}
  />
</Story>

<Story name="One Moved To Another Number" asChild>
  <ExtensionPorts
    asked={[web, syslog]}
    listening={[{ ...web, port: 9090 }, syslog]}
    busy={false}
    {onsave}
  />
</Story>

<Story name="Saving" asChild>
  <ExtensionPorts asked={[web]} listening={[web]} busy {onsave} />
</Story>

<Story name="Locked" asChild>
  <ExtensionPorts
    asked={[web]}
    listening={[web]}
    locked="This extension is declared in the configuration file."
    busy={false}
    {onsave}
  />
</Story>

<Story name="Refused" asChild>
  <ExtensionPorts
    asked={[web]}
    listening={[]}
    busy={false}
    fault={new Error('port already in use')}
    {onsave}
  />
</Story>
