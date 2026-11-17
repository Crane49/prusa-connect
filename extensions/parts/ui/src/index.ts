// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
//
// `defineModule` is an identity function. It exists so an author gets the
// host's types at this call site without importing the host.
import { DeviceStates } from '@prusa/connect-ui/data';
import { defineModule } from '@prusa/connect-ui/slots';

import { Catalogue } from './catalogue.svelte';
import ChosenCard from './cards/chosen-card.svelte';
import ChosenCardEditor from './cards/chosen-card-editor.svelte';
import FolderCard from './cards/folder-card.svelte';
import FolderCardEditor from './cards/folder-card-editor.svelte';
import { filesOn } from './files';
import { FolderDrop } from './folder-drop.svelte';
import { Navigation } from './navigation.svelte';
import { PartsPage } from './parts-page';
import { partsOn } from './parts';
import { holdSession } from './session';
import { TreeState } from './tree-state.svelte';
import './styles.css';

export default defineModule((host) => {
  // Everything the page and the cards need is built here, from the host and
  // from nothing else. The registrations are held to `contributes` in
  // module.yaml: a path or a card the manifest does not declare is refused
  // and this module is dropped whole.
  const files = filesOn(host);
  const catalogue = new Catalogue(partsOn(host), files, host.principal.id);

  holdSession({
    host,
    files,
    catalogue,
    // The address bar is the browser's, and a page rendered anywhere else has
    // none, which is what `null` says.
    navigation: new Navigation(typeof window === 'undefined' ? null : window),
    states: new DeviceStates(host.client),
    tree: new TreeState(host.store),
    drop: new FolderDrop(catalogue, files),
  });

  host.register.page('parts', PartsPage);
  // The dashboard cards, held to `contributes.cards` in module.yaml. Each
  // brings its own settings editor (ADR-20261008-hna rule 20).
  host.register.card('folder', FolderCard, { editor: FolderCardEditor });
  host.register.card('chosen', ChosenCard, { editor: ChosenCardEditor });
});
