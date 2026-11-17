### Changed
- [sdk] A `module.yaml` is refused when its `license`, or the `why` or `sends` of an `egress` entry, holds a control or invisible character. Such a character is a terminal escape, a reversal of the writing direction or a zero-width space.
