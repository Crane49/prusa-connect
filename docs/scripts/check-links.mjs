// Checks every internal link under docs/ against the readers each file has.
//
// A published page has two readers and is checked against both. A page the site
// does not publish has one, the vault, and is checked against that one. The
// second half exists because the first half read 51 of the 178 markdown files
// under docs/: the whole of docs/decisions/,
// plus CHANGELOG.md and map.md, sat outside the content
// collection and therefore outside every link gate in the repository. One dead
// link was live in that gap when it was measured.
//
//   site    the built Starlight site. A link resolves as a URL against the
//           page's own URL, and its target must be a route the build emitted.
//           Anchors are read from the built HTML, so an explicit `{#id}` and a
//           slugged heading are both ground truth rather than a reimplementation
//   vault   the Obsidian vault, which is the whole repository. A link resolves
//           as a filesystem path against the source file's own directory, and
//           its target must be a file that exists
//
// The two readers disagree by construction: the site serves only what is under
// the content directory, so a page can legitimately link to docs/decisions/ for
// the vault reader and that link can never work on the site. That is why a
// verdict is a pair and not a boolean.
//
// Zero dependencies: node builtins only, so it runs before `pnpm install`.

import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { resolve, dirname, relative, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const docsRoot = resolve(here, '..');
const repoRoot = resolve(docsRoot, '..');
const contentDir = resolve(docsRoot, 'src/content/docs');

// The site is served under this prefix. astro.config.mjs sets `base: '/docs'`.
const BASE = '/docs';

// Routes the Astro build does not emit, produced by a later step of
// `pnpm run build:static`. `swagger/` comes from docs/scripts/build-swagger.mjs,
// which writes public/swagger/ that Astro then copies verbatim.
const EXTERNALLY_PRODUCED_ROUTES = new Set(['swagger/']);

// Directories under docs/ that hold no authored markdown: the toolchain's own
// tree, and the build output the site half of this check reads.
const NOT_AUTHORED = new Set(['node_modules', 'dist', '.astro']);

// A released changelog entry is never edited, and some cite a decision record
// by the number it had before the records took dated ids. The file has a new
// name now, so such a link is history and not a link to follow.
const HISTORICAL_RECORD_LINK = /(^|\/)decisions\/\d{4}-[a-z0-9-]+\.md(#|$)/;

// The floor under the unpublished half. It read 127 files the day it was
// written. A number well under that fails a walk that stopped reaching them,
// which is the failure the published half already had a floor against.
const UNPUBLISHED_FLOOR = 60;

const args = process.argv.slice(2);

// The links that resolve for both readers are counted rather than listed. There
// are 245 of them, and an output nobody finishes is an output nobody reads the
// failures in.
const verbose = args.includes('--verbose');
const distDir = argValue('--dist') ? resolve(process.cwd(), argValue('--dist')) : resolve(docsRoot, 'dist');

function argValue(name) {
  const at = args.indexOf(name);

  return at >= 0 && args[at + 1] ? args[at + 1] : null;
}

// ── reading the tree ────────────────────────────────────────────────────────

function walk(dir, keep) {
  const found = [];

  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);

    if (statSync(full).isDirectory()) {
      found.push(...walk(full, keep));
    } else if (keep(full)) {
      found.push(full);
    }
  }

  return found;
}

// unpublishedPages is every markdown file under docs/ that the content
// collection does not hold, so the site emits no route for it and only the
// vault reader applies. The list comes from git, because a page git ignores,
// such as a local plan under docs/superpowers/, is not part of the tree this
// gate judges. Untracked pages git does not ignore stay in.
function unpublishedPages() {
  const listed = execFileSync(
    'git',
    ['ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', '*.md', '*.mdx'],
    { cwd: docsRoot, encoding: 'utf8' },
  );

  return listed
    .split('\0')
    .filter((path) => path && !path.split('/').some((part) => NOT_AUTHORED.has(part)))
    .map((path) => join(docsRoot, path))
    .filter((full) => existsSync(full) && relative(contentDir, full).startsWith('..'))
    .sort();
}

// stripCode removes fenced blocks and inline spans so a link written inside an
// example is not reported as a link, and a `# heading` inside a shell block is
// not reported as a heading.
function stripCode(body) {
  return stripFences(body).map((line) => line.replace(/`[^`]*`/g, '')).join('\n');
}

// stripFences blanks fenced blocks and keeps everything else, inline spans
// included. Headings read the fenced form and not stripCode's, because a
// heading is often mostly inline code: `### `module_state` and `sdk/stateapi``
// arrived here as `###  and `, and every link to it was reported broken against
// an anchor the gate had thrown away. Three were, in ADR-20260827-wmv, ADR-20260827-rwq and
// ADR-20260827-2bp.
function stripFences(body) {
  const kept = [];
  let fenced = false;

  for (const line of body.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) {
      fenced = !fenced;
      kept.push('');
      continue;
    }

    kept.push(fenced ? '' : line);
  }

  return kept;
}

// frontmatterSlug reads a top-level `slug:` out of the frontmatter block. Only
// depth zero counts: `sidebar:` has nested keys and none of them is the route.
function frontmatterSlug(body) {
  const match = /^---\r?\n([\s\S]*?)\r?\n---/.exec(body);

  if (!match) return null;

  for (const line of match[1].split('\n')) {
    const found = /^slug:\s*(.+?)\s*$/.exec(line);

    if (found) return found[1].replace(/^["']|["']$/g, '');
  }

  return null;
}

// routeFor is Starlight's route derivation: the path under the content
// directory without its extension, with `index` naming its parent, unless the
// page overrides it with `slug`.
function routeFor(file, body) {
  const override = frontmatterSlug(body);

  if (override !== null) return override.replace(/^\/+|\/+$/g, '');

  let slug = relative(contentDir, file).split(/[\\/]/).join('/');
  slug = slug.replace(/\.mdx?$/, '');
  slug = slug.replace(/(^|\/)index$/, '');

  return slug.replace(/^\/+|\/+$/g, '');
}

// ── the site reader ─────────────────────────────────────────────────────────

// idsIn pulls every element id out of one built page. Anchors published by a
// generated reference are set by the rehype plugin in astro.config.mjs, so
// reading the output is the only derivation that cannot drift from it.
function idsIn(html) {
  const ids = new Set();

  for (const match of html.matchAll(/\sid="([^"]+)"/g)) ids.add(match[1]);

  return ids;
}

const pageCache = new Map();

function sitePage(routePath) {
  if (pageCache.has(routePath)) return pageCache.get(routePath);

  const candidates = [
    join(distDir, routePath, 'index.html'),
    join(distDir, routePath),
  ];

  let page = null;

  for (const candidate of candidates) {
    if (existsSync(candidate) && statSync(candidate).isFile()) {
      const html = candidate.endsWith('.html') ? readFileSync(candidate, 'utf8') : null;

      page = { file: candidate, html, ids: html === null ? null : idsIn(html) };
      break;
    }
  }

  pageCache.set(routePath, page);

  return page;
}

// ── the vault reader ────────────────────────────────────────────────────────

// headingAnchorsIn is every name a heading in a markdown file answers to.
// Obsidian addresses a heading by its text; the site and GitHub address it by a
// slug of that text; ADR-20260826-swm pages set an explicit `{#id}`. All three are
// accepted, because a link is only wrong here when it names a heading that is
// not on the page at all.
function headingAnchorsIn(body) {
  const anchors = new Set();

  for (const line of stripFences(body)) {
    const match = /^#{1,6}\s+(.+?)\s*$/.exec(line);

    if (!match) continue;

    let text = match[1];
    const explicit = /\{#([^}]+)\}\s*$/.exec(text);

    if (explicit) {
      anchors.add(explicit[1]);
      text = text.slice(0, explicit.index);
    }

    const prose = text
      .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
      // `*` and a backtick are markup. `_` is not stripped: the slug step below
      // keeps underscores, so stripping them here made every heading naming an
      // identifier unreachable. `### `module_state` and `sdk/stateapi`` slugged
      // to modulestate-and-sdkstateapi, and ADR-20260827-rwq's link to it was reported
      // broken against the anchor github-slugger actually emits.
      .replace(/[*`]/g, '')
      .trim();

    anchors.add(prose.toLowerCase());
    anchors.add(
      prose
        .toLowerCase()
        .replace(/[^\p{L}\p{N}\s\-_]/gu, '')
        .trim()
        .replace(/\s+/g, '-'),
    );
  }

  return anchors;
}

const vaultCache = new Map();

function vaultAnchors(file) {
  if (vaultCache.has(file)) return vaultCache.get(file);

  const anchors = /\.mdx?$/.test(file) ? headingAnchorsIn(readFileSync(file, 'utf8')) : null;

  vaultCache.set(file, anchors);

  return anchors;
}

// ── extracting links ────────────────────────────────────────────────────────

// linksIn finds every markdown link and every raw `<a href>` in one page, with
// the line it sits on. External schemes are not this gate's business.
function linksIn(body) {
  const found = [];
  const lines = stripCode(body).split('\n');

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    const patterns = [/\]\(\s*([^)\s]+?)\s*(?:\s+"[^"]*")?\)/g, /<a\s[^>]*href="([^"]+)"/g];

    for (const pattern of patterns) {
      for (const match of line.matchAll(pattern)) {
        const href = match[1];

        if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('//')) continue;

        found.push({ href, line: index + 1 });
      }
    }
  }

  return found;
}

function splitFragment(href) {
  const at = href.indexOf('#');

  if (at < 0) return { path: href, fragment: '' };

  return { path: href.slice(0, at), fragment: decodeURIComponent(href.slice(at + 1)) };
}

// ── verdicts ────────────────────────────────────────────────────────────────

// siteVerdict resolves one link the way a browser on the built site would, and
// says whether that URL is a page the build emitted.
//
// A relative `.md` target inside the content directory is rewritten to its
// route by `rehypeRelativeMarkdownLinks` in astro.config.mjs. This does not
// recompute that rewrite and trust itself: it reads the built page and requires
// the rewritten href to be in it, so removing the plugin fails here rather than
// leaving a green gate over 172 links that 404.
function siteVerdict(link, pageUrl, sourceFile, sourceHtml) {
  const { path: linkPath, fragment } = splitFragment(link.href);

  if (/\.mdx?$/.test(linkPath) && !linkPath.startsWith('/')) {
    const target = resolve(dirname(sourceFile), decodeURIComponent(linkPath));

    if (!relative(contentDir, target).startsWith('..') && existsSync(target)) {
      const route = routeFor(target, readFileSync(target, 'utf8'));
      const url = route === '' ? `${BASE}/` : `${BASE}/${route}/`;
      // The prefix, not the whole href: the fragment is copied verbatim by the
      // plugin and is checked against the target's real ids below. Matching the
      // whole href would report a bad anchor as a missing plugin.
      if (sourceHtml === null || !sourceHtml.includes(`href="${url}`)) {
        return {
          ok: false,
          why: `the build did not rewrite this to ${url}. Is rehypeRelativeMarkdownLinks still registered in astro.config.mjs, and is docs/dist current?`,
          target: url,
        };
      }

      const page = sitePage(route);

      if (!page) return { ok: false, why: `${url} is not a built route`, target: url };

      if (fragment && page.ids && !page.ids.has(fragment)) {
        return { ok: false, why: `no #${fragment} on ${url}`, target: url };
      }

      return { ok: true, target: url };
    }
  }

  if (link.href.startsWith('#')) {
    const page = sitePage(pageUrl.slice(BASE.length + 1));

    if (!page || !page.ids) return { ok: false, why: `${pageUrl} was not built` };

    return page.ids.has(fragment)
      ? { ok: true, target: pageUrl }
      : { ok: false, why: `no #${fragment} on ${pageUrl}`, target: pageUrl };
  }

  const resolved = new URL(link.href, `https://site${pageUrl}`);
  const url = decodeURIComponent(resolved.pathname);

  if (!url.startsWith(`${BASE}/`) && url !== BASE) {
    return { ok: false, why: `${url} is outside the site base ${BASE}`, target: url };
  }

  const routePath = url.slice(BASE.length).replace(/^\/+/, '');

  if (EXTERNALLY_PRODUCED_ROUTES.has(routePath)) return { ok: true, target: url, unchecked: true };

  const page = sitePage(routePath);

  if (!page) return { ok: false, why: `${url} is not a built route`, target: url };

  if (fragment && page.ids && !page.ids.has(fragment)) {
    return { ok: false, why: `no #${fragment} on ${url}`, target: url };
  }

  return { ok: true, target: url };
}

// vaultVerdict resolves one link the way Obsidian would, against the source
// file's own directory. A leading slash is neither Obsidian's relative form nor
// its vault-absolute form (which carries no slash), so it is resolved from the
// vault root and reported under its own class.
function vaultVerdict(link, sourceFile) {
  const { path: rawPath, fragment } = splitFragment(link.href);

  let target;
  let rootAbsolute = false;

  if (rawPath === '') {
    target = sourceFile;
  } else if (rawPath.startsWith('/')) {
    rootAbsolute = true;
    target = join(repoRoot, decodeURIComponent(rawPath).replace(/^\/+/, ''));
  } else {
    target = resolve(dirname(sourceFile), decodeURIComponent(rawPath));
  }

  const shown = relative(repoRoot, target).split(/[\\/]/).join('/');

  // A directory resolves for the reader browsing the repository, which is what
  // docs/map.md links to when it names `decisions/`. It carries no headings, so
  // a fragment against one never resolves.
  if (!existsSync(target) || (!statSync(target).isFile() && !(statSync(target).isDirectory() && !fragment))) {
    return { ok: false, why: `${shown} is not a file in the vault`, target: shown, rootAbsolute };
  }

  if (fragment) {
    const anchors = vaultAnchors(target);

    if (anchors && !anchors.has(fragment) && !anchors.has(fragment.toLowerCase())) {
      return { ok: false, why: `no heading #${fragment} in ${shown}`, target: shown, rootAbsolute };
    }
  }

  return { ok: true, target: shown, rootAbsolute };
}

// ── the run ─────────────────────────────────────────────────────────────────

function main() {
  if (!existsSync(contentDir)) fail(`no content directory at ${contentDir}`);

  if (!existsSync(distDir)) {
    fail(
      `no built site at ${distDir}.\n` +
        `  The site half of this check reads the build, so it cannot run without one.\n` +
        `  Run \`make docs-build-check\` first, or \`cd docs && pnpm run build\`.`,
    );
  }

  const files = walk(contentDir, (f) => /\.mdx?$/.test(f)).sort();

  if (files.length === 0) fail(`no pages under ${relative(repoRoot, contentDir)}`);

  const buckets = { both: [], siteOnly: [], vaultOnly: [], neither: [] };
  const unbuilt = [];
  let links = 0;

  for (const file of files) {
    const body = readFileSync(file, 'utf8');
    const route = routeFor(file, body);
    const pageUrl = route === '' ? `${BASE}/` : `${BASE}/${route}/`;
    const shownFile = relative(repoRoot, file).split(/[\\/]/).join('/');

    const built = sitePage(route);

    if (!built) {
      unbuilt.push(`${shownFile} derives route ${pageUrl}, which the build did not emit`);
      continue;
    }

    for (const link of linksIn(body)) {
      links++;

      const site = siteVerdict(link, pageUrl, file, built.html);
      const vault = vaultVerdict(link, file);
      const record = { where: `${shownFile}:${link.line}`, href: link.href, site, vault };

      if (site.ok && vault.ok) buckets.both.push(record);
      else if (site.ok) buckets.siteOnly.push(record);
      else if (vault.ok) buckets.vaultOnly.push(record);
      else buckets.neither.push(record);
    }
  }

  // A vault-only link splits three ways, and the split is what decides whether
  // it is a defect or a choice nobody has taken yet.
  //
  //   byConstruction  a relative path to a repository file outside the content
  //                   directory. It resolves in the vault, and the site cannot
  //                   serve that file under any link form, so no form is better
  //   rootAbsolute    the same target written with a leading slash. It 404s on
  //                   the site, and a leading slash is neither of the two forms
  //                   Obsidian documents. Rewriting it relative costs the site
  //                   nothing, because the site could not serve the target
  //                   either way, and buys the vault reader a link that works.
  //                   That is why this one is a hard failure and the next is not
  //   conflicting     the target is a page the site does publish. Writing the
  //                   link so the site resolves it kills it in the vault and the
  //                   reverse, which is the same conflict the site-only bucket
  //                   holds from the other side. Both wait on one decision
  const byConstruction = [];
  const rootAbsolute = [];
  const vaultOkSiteDead = [];

  for (const record of buckets.vaultOnly) {
    const abs = resolve(repoRoot, record.vault.target);
    const inside = !relative(contentDir, abs).startsWith('..');

    if (record.vault.rootAbsolute && !inside) rootAbsolute.push(record);
    else if (inside) vaultOkSiteDead.push(record);
    else byConstruction.push(record);
  }

  // A site-only link is legitimate when its target is not a document at all.
  // `/docs/swagger/` is a Swagger UI bundle that build-swagger.mjs writes, and
  // no file in the vault corresponds to it.
  const siteOnly = [];
  const siteOnlyByConstruction = [];

  for (const record of buckets.siteOnly) {
    const routePath = record.site.target.slice(BASE.length).replace(/^\/+/, '');

    (EXTERNALLY_PRODUCED_ROUTES.has(routePath) ? siteOnlyByConstruction : siteOnly).push(record);
  }

  // The other 127 files. One reader, so one verdict, and a link that does not
  // resolve is a defect with nothing to weigh it against.
  const unpublished = unpublishedPages();

  if (unpublished.length < UNPUBLISHED_FLOOR) {
    fail(
      `the unpublished half read ${unpublished.length} file(s) under ${relative(repoRoot, docsRoot)}, ` +
        `under its floor of ${UNPUBLISHED_FLOOR}, so this run proved nothing.\n` +
        `  Either those files moved, or the walk stopped reaching them.`,
    );
  }

  const vaultDead = [];
  let vaultLinks = 0;

  for (const file of unpublished) {
    const body = readFileSync(file, 'utf8');
    const shownFile = relative(repoRoot, file).split(/[\\/]/).join('/');

    for (const link of linksIn(body)) {
      if (shownFile === 'docs/CHANGELOG.md' && HISTORICAL_RECORD_LINK.test(link.href)) continue;

      vaultLinks++;

      const vault = vaultVerdict(link, file);

      if (!vault.ok) vaultDead.push({ where: `${shownFile}:${link.line}`, href: link.href, vault });
    }
  }

  report({
    files: files.length,
    links,
    buckets,
    byConstruction,
    rootAbsolute,
    vaultOkSiteDead,
    siteOnly,
    siteOnlyByConstruction,
    unbuilt,
    unpublished: unpublished.length,
    vaultLinks,
    vaultDead,
  });

  const broken =
    unbuilt.length + buckets.neither.length + rootAbsolute.length + siteOnly.length + vaultOkSiteDead.length + vaultDead.length;

  if (broken > 0) {
    process.stderr.write(`\nFAIL ${broken} link(s) do not resolve for both readers of this tree.\n`);
    process.exit(1);
  }

  process.stdout.write(
    `\nOK ${links} internal link(s) across ${files.length} page(s) resolve for the site and for the vault, ` +
      `or name a target only one of them can serve. ` +
      `${vaultLinks} more across ${unpublished.length} unpublished page(s) resolve for the vault.\n`,
  );
}

function list(title, records, describe) {
  if (records.length === 0) return;

  process.stdout.write(`\n${title} (${records.length})\n`);

  for (const record of records) {
    process.stdout.write(`  ${record.where}\n    ${record.href}\n      ${describe(record)}\n`);
  }
}

function report(result) {
  process.stdout.write(
    `docs link check: ${result.links} internal link(s) across ${result.files} page(s), against ${relative(repoRoot, distDir).split(/[\\/]/).join('/')}\n`,
  );
  process.stdout.write(
    `  both readers ${result.buckets.both.length}` +
      `  site only ${result.buckets.siteOnly.length}` +
      `  vault only ${result.buckets.vaultOnly.length}` +
      `  neither ${result.buckets.neither.length}\n`,
  );

  if (result.unbuilt.length > 0) {
    process.stdout.write(`\npages the build did not emit (${result.unbuilt.length})\n`);
    for (const line of result.unbuilt) process.stdout.write(`  ${line}\n`);
  }

  list('BROKEN, resolves for neither reader', result.buckets.neither, (r) => `site: ${r.site.why}\n      vault: ${r.vault.why}`);
  list(
    'BROKEN, a leading slash where the relative form would work and cost the site nothing',
    result.rootAbsolute,
    (r) => `site: ${r.site.why}\n      fix: ${r.vault.target}, written relative to this page`,
  );
  list(
    'BROKEN in the vault. Write it as a relative path to the .md file; the build rewrites that to the route',
    result.siteOnly,
    (r) => `vault: ${r.vault.why}`,
  );
  list('BROKEN on the site, and the site does publish this page', result.vaultOkSiteDead, (r) => `site: ${r.site.why}`);

  process.stdout.write(
    `\nunpublished pages under docs/: ${result.vaultLinks} internal link(s) across ${result.unpublished} file(s), ` +
      `read against the vault only\n`,
  );

  list(
    'BROKEN in the vault, on a page the site does not publish',
    result.vaultDead,
    (r) => `vault: ${r.vault.why}`,
  );

  if (!verbose) {
    process.stdout.write(
      `\n  ${result.byConstruction.length} link(s) name a repository file the site cannot serve, ` +
        `and ${result.siteOnlyByConstruction.length} name a site route with no file behind it. ` +
        `Both are correct. Pass --verbose to list them.\n`,
    );

    return;
  }

  list('vault only by construction, the site cannot serve this target', result.byConstruction, (r) => `site: ${r.site.why}`);
  list('site only by construction, no file in the vault corresponds to this route', result.siteOnlyByConstruction, (r) => `vault: ${r.vault.why}`);
}

function fail(message) {
  process.stderr.write(`docs link check: ${message}\n`);
  process.exit(1);
}

main();
