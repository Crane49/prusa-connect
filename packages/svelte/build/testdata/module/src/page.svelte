<script lang="ts">
  let spools = $state(0);
</script>

<!-- Two classes the test reads off the built bundle. `gap-x-7` is named nowhere
     else in this repository, so a rule for it can only have come from this
     module's own Tailwind build (ADR-20260914-244 rule 10). `bg-state-idle` exists only
     in the library's theme, so a rule for it says the second `@reference`
     reached the compiler (rule 12). `translate-x-1` makes Tailwind emit an
     `@property`, which the build has to keep outside the scope (rule 14). -->
<div class="flex translate-x-1 gap-x-7">
  <button class="bg-state-idle" onclick={() => spools++}>{spools} spools</button
  >
</div>
