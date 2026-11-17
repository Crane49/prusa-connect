---
title: Pending changes
description: "The news no release has shipped yet: one file per change, which the next release files into the changelog and deletes."
status: living
tags:
  - changelog
  - release
---
# Pending changes

This folder holds the news that no release has shipped yet. Each change that a
self-hoster or an extension author can observe adds one file here. The next
release files every file into [the changelog](../CHANGELOG.md) under its own
version, and deletes the files. [ADR-20261021-arm](../decisions/20261021-arm-pending-news-is-a-file-per-change.md)
holds the rules.

A change adds a new file and edits no file that another change edits. That is
why two pull requests never conflict over the news.

## Writing a fragment

```sh
make changelog-new NAME=nearby-printers CATEGORY=Changed
```

This writes `docs/changes/<date>-<time>-nearby-printers.md` with the time it
is now in UTC. Run it once per pull request, with the first commit that has an
entry. Replace the empty bullet with the entry:

```markdown
### Changed
- The list of printers nearby moved from the Info tab of a printer's page to the Printers page, under the listing.
```

- A file holds one or more `### <category>` headings with bullets under each.
  The categories are Security, Removed, Changed, Deprecated, Added and Fixed.
- An entry is written as [changelog-discipline](../../.agents/rules/changelog-discipline.md)
  says. The first sentence is also the commit's `Changelog:` trailer.
- One pull request writes one file, with every entry it ships, however many
  commits it takes. A later commit adds its entry to that file, or rewords
  one there, and adds no file of its own. A pull request whose commits all
  carry `Changelog: none` writes none.
- A relative link starts with `../`, because this folder is one below the
  changelog. The release takes one `../` off when it files the entry.

## Reading what the next release ships

```sh
make changelog-preview
```

This prints the section the next release would file, and changes nothing.
