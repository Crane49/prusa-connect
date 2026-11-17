<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { catalogueOf, outcomeOf } from '../../../../test/extension-fixtures';
  import InstallOutcome from './install-outcome.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Install Outcome',
    component: InstallOutcome,
  });

  const outcome = (...args: Parameters<typeof outcomeOf>) =>
    catalogueOf({ outcome: outcomeOf(...args) });
</script>

<Story name="Installed And Running" asChild>
  <InstallOutcome catalogue={outcome()} name="Spool tracker" />
</Story>

<Story name="Updated With New Requests" asChild>
  <InstallOutcome
    catalogue={outcome({ added: ['permission jobs.write'] }, true)}
    name="Spool tracker"
  />
</Story>

<Story name="Requests Withheld" asChild>
  <InstallOutcome
    catalogue={outcome({
      withheld: ['listen tcp 8080 web: serves its own page'],
    })}
    name="Spool tracker"
  />
</Story>

<Story name="Needs A Restart" asChild>
  <InstallOutcome
    catalogue={outcome({ restart: true, running: false })}
    name="Spool tracker"
  />
</Story>

<Story name="Switched Off" asChild>
  <InstallOutcome
    catalogue={outcome({ enabled: false, running: false, state: 'disabled' })}
    name="Spool tracker"
  />
</Story>

<Story name="Not Running" asChild>
  <InstallOutcome
    catalogue={outcome({ running: false, state: 'failed' })}
    name="Spool tracker"
  />
</Story>

<Story name="Nothing To Report" asChild>
  <InstallOutcome catalogue={catalogueOf()} name="Spool tracker" />
</Story>
