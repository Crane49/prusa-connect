// The bundle's entry. The app imports this file and calls what it default
// exports, handing it the one object a module receives.
import { defineModule } from '@prusa/connect-ui/slots';

import CameraSection from './camera-section.svelte';
import CameraView from './camera-view.svelte';
import { hold } from './held';
import SettingsPage from './settings-page.svelte';
import './styles.css';

export default defineModule((host) => {
  hold(host);

  // The three the manifest declares under contributes.
  host.register.settings('cameras', SettingsPage);
  host.register.cameraSection('buddy3d', CameraSection);
  host.register.cameraView('buddy3d', CameraView);
});
