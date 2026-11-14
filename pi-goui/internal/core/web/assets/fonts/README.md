# Embedded terminal fonts

All faces are served locally and embedded in the Go binary; no CDN, runtime font installation or local-font lookup is used. Font outlines and character coverage are retained, including Nerd Font icons.

- `jetbrains-mono-{regular,bold,italic,bold-italic}.woff2`: JetBrainsMono Nerd Font Mono, JetBrains Mono 2.304 patched by Nerd Fonts 3.5.1. Converted with `woff2_compress` from the matching `/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-*.ttf` system fonts. Copyright 2020 The JetBrains Mono Project Authors. SIL Open Font License 1.1; see `JetBrainsMono-OFL.txt` and `NerdFonts-LICENSE.txt`.
- `wqy-microhei-mono.woff2`: WenQuanYi Micro Hei Mono 0.2.0-beta, the host system's default monospace face, used for Chinese fallback. Extracted face 1 from `/usr/share/fonts/wenquanyi/wqy-microhei/wqy-microhei.ttc` using `fontTools.ttLib.TTFont(..., fontNumber=1).save(...)`, then converted with `woff2_compress`. Digitized data copyright © 2007 Google Corporation. Copyright © 2008–2009 WenQuanYi Board of Trustees and Qianqian Fang. Distributed under its embedded Apache License 2.0 option; see `WenQuanYi-Apache-2.0.txt`.

The page and xterm use the same family stack. All four Nerd Font styles and the CJK fallback finish loading before xterm measures cells, opens, fits or connects the PTY, avoiding font-swap geometry drift.
