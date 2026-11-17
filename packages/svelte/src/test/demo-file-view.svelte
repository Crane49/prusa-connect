<!-- A module's view of a file, as a designer reads it in a story: what a
     project holds, drawn with the library's own parts. -->
<script lang="ts">
  import { KeyValueList } from '../primitives';

  let { fileRef }: { fileRef: string } = $props();
</script>

<div data-file-ref={fileRef}>
  <KeyValueList
    rows={[
      { id: 'plates', label: 'Plates', value: '2' },
      { id: 'objects', label: 'Objects', value: 'Hull, Chimney, Cabin' },
      { id: 'printer', label: 'Made for', value: 'MK4S' },
    ]}
  />
</div>
