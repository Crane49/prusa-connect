<!-- A screen for the app's error boundary: it counts its mounts and fails to
     render while `screen.failing` is set. -->
<script lang="ts">
  import { screen } from './screen-probe';

  screen.mounts += 1;
  if (screen.failing) throw new Error('this screen cannot be drawn');
</script>

<div data-testid="counting-screen"></div>
