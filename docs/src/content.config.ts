import { defineCollection, z } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

// ADR-20260826-53y rule 1. A page declares the reader it assumes, and rule 3 reads that
// value to decide which repository paths its body may name.
//
//   run     a self-hoster: a URL, a config key, a flag, a port, an error code
//   extend  the above, plus an import path under `sdk/` and a descriptor field
//   build   the above, plus a path under `internal/` and a line number
//
// There is no `mixed`, by rule 5: a gate handed two tiers has no restriction
// left to apply, so a page two readers need becomes two pages.
const audience = z.enum(['run', 'extend', 'build']);

// These pages were written against the implementation that now lives in
// `legacy/`, so a reader cannot otherwise tell historical behaviour from
// current behaviour. `rewrite` records which a page is. See
// docs/decisions/20260824-dym-legacy-quarantine.md.
//
//   describes      legacy    the legacy implementation; a specification the rewrite must honour
//                  current   unaffected by the rewrite: a protocol, a file format, a
//                            printer-side behaviour, the container, a tool
//                  superseded a design a later decision has replaced
//                  mixed     both, section by section; the page says which
//   implementedBy  the part of the tree that implements it today
const rewrite = z.object({
  describes: z.enum(['legacy', 'current', 'superseded', 'mixed']),
  implementedBy: z.array(z.string()).optional(),
});

export const collections = {
	docs: defineCollection({
		loader: docsLoader(),
		schema: docsSchema({ extend: z.object({ audience, rewrite }) }),
	}),
};
