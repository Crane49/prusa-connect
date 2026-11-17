// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
//
// The registrations are held to module.yaml: `contributes.file_view` names
// the view `project` for .3mf files, and `ui.exports` names the component
// `project-view`. A name the manifest does not declare is refused and this
// module is dropped whole.
import { defineModule } from '@prusa/connect-ui/slots';

import { projectsOn } from './api';
import { holdTranslate } from './i18n';
import { ProjectFile } from './project-file';
import { ProjectFileView } from './project-file-view';
import { Projects } from './projects.svelte';
import { holdProjects } from './session';
import './styles.css';

export default defineModule((host) => {
  holdTranslate(host.t);
  holdProjects(new Projects(projectsOn(host.client)), host.signal);

  host.register.fileView('project', ProjectFileView);
  // The same view, for a page of another module that draws a project, such
  // as one that plans prints of the ticked copies (ADR-20261027-wvj rule 21).
  host.register.component('project-view', ProjectFile);
});
