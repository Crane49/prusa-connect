<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fakeListingStore } from '../../test/fakes';
  import {
    denied,
    fakePeopleSource,
    oneAdmin,
    people,
    solePerson,
    SUBJECT,
  } from './people.fixtures';
  import SettingsPeopleScreen from './settings-people-screen.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings People',
    component: SettingsPeopleScreen,
  });

  const store = fakeListingStore();

  /** A store already holding the stacked view, so the second shape has a story. */
  const stackedStore = {
    read: () => JSON.stringify({ view: 'rows' }),
    write: () => undefined,
  };
</script>

<!--
  The React specification's own story titles, so the two sets line up: the
  listing, the load, the fresh server, the rows view and the row that cannot
  be demoted.
-->

<Story name="Listing" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(people)}
    subject={SUBJECT}
    {store}
  />
</Story>

<Story name="Loading" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource([], { loading: true, loaded: false })}
    subject={SUBJECT}
    {store}
  />
</Story>

<Story name="Only The Setup Account" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(solePerson)}
    subject={SUBJECT}
    {store}
  />
</Story>

<Story name="The Rows View" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(people)}
    subject={SUBJECT}
    store={stackedStore}
  />
</Story>

<Story name="The Last Administrator Cannot Be Demoted" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(oneAdmin)}
    subject={SUBJECT}
    {store}
  />
</Story>

<Story name="A Write Is In Flight" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(people, { writing: true })}
    subject={SUBJECT}
    {store}
  />
</Story>

<Story name="Nobody Can Sign In Yet" asChild>
  <SettingsPeopleScreen people={fakePeopleSource([])} {store} />
</Story>

<Story name="The Read Was Refused" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource([], { loaded: false, error: denied })}
    {store}
  />
</Story>

<Story name="A Write Was Refused" asChild>
  <SettingsPeopleScreen
    people={fakePeopleSource(people, { error: denied })}
    subject={SUBJECT}
    {store}
  />
</Story>
