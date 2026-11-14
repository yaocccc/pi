# Vendored terminal runtime

- `@xterm/xterm` 6.0.0: `xterm.js`, `xterm.css`, `xterm.LICENSE`.
- `@xterm/addon-fit` 0.11.0: `addon-fit.js`, `addon-fit.LICENSE`.
- `@xterm/addon-unicode-graphemes` 0.4.0: `addon-unicode-graphemes.js`, `addon-unicode-graphemes.LICENSE`.

These npm package distributions are embedded and served locally. No npm build, CDN or package installation is required at runtime. Unshipped source-map references are removed.

The grapheme addon is upstream experimental; its cluster handling is needed to keep skin tones, flags, variation selectors and ZWJ emoji in the same columns as the Go TUI. Its width lookup is locally patched with `gouiWideRanges`, generated from `github.com/charmbracelet/x/ansi.StringWidth(string(r)) == 2` for all Unicode scalar values used by the TUI. This closes newer Emoji/CJK width gaps in the bundled data without changing ambiguous characters to double width. Otherwise-unknown supplementary pictographs in those ranges retain extended-pictographic cluster behavior.

`internal/core/web/testdata/unicode-wide-ranges.json` records the generated ranges. `TestTerminalWideRuneTable` detects changes to the Go width rules; the Chromium test checks all generated wide ranges, plus visible frame-edge alignment for Emoji, Chinese, Nerd icons, keycaps, flags and combined sequences. Regenerate the fixture and `gouiWideRanges` together if width dependencies change.
