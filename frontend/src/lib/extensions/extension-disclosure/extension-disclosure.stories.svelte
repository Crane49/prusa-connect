<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ExtensionDisclosure from './extension-disclosure.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Disclosure',
    component: ExtensionDisclosure,
  });
</script>

<Story name="Licence And Connections" asChild>
  <ExtensionDisclosure
    license="OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1"
    egressKnown={true}
    egress={[
      {
        host: 'api.github.com',
        ports: [443],
        from: 'process',
        why: 'Lists the PrusaSlicer releases every six hours.',
        sends: 'The request, and the github_token setting when one is set.',
      },
      {
        option: 'manifest_url',
        ports: [443],
        from: 'process',
        why: "Reads the operator's manifest of builds.",
        sends: 'Only the request for the manifest.',
      },
      {
        host: '*.example.net',
        ports: [443, 8443],
        from: 'ui',
        why: 'Shows the map tiles.',
        sends: 'Nothing but the request.',
      },
    ]}
  />
</Story>

<Story name="Connects Nowhere" asChild>
  <ExtensionDisclosure license="MIT" egressKnown={true} egress={[]} />
</Story>

<Story name="Says Nothing" asChild>
  <ExtensionDisclosure license={undefined} egressKnown={false} egress={[]} />
</Story>
