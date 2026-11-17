<!-- A dashboard card that works, for the slot tests. -->
<script lang="ts">
  import type { CardProps } from '../slots/card-types';

  let { config, title, size, editing }: CardProps = $props();
</script>

<p data-testid="plain-card">
  {title === '' ? 'Plain card' : title}, {String(config.label)}, {size.w}×{size.h}{editing
    ? ', editing'
    : ''}
</p>
