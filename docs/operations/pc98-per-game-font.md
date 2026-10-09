# PC-98 per-game bitmap fonts

Release 1.02 supports a dedicated font for each ROM/version without changing ROM bytes.
Selection priority within the source media's own directory:

1. `<full-ROM-filename>.font.json`, for example `GameCN.hdi.font.json` containing `{"file":"MyFont.bmp"}`.
2. `<ROM-stem>.font.bmp`, for example `GameCN.font.bmp`.
3. `FONT.bmp`, then legacy `PC98_CN.bmp`.

Names are case-insensitive. Candidates must be unambiguous regular sibling files,
not paths or symlinks. Fonts must be uncompressed 2048x2048 1-bit BMPs, at most
1 MiB; the JSON sidecar is limited to 4 KiB. Missing/invalid candidates fall back
to the next rule. Arbitrary sibling BMPs and parent-directory fonts are not guessed.

Exactly one active font is published per game; grouped media are considered in
launch order. Its logical manifest name is `FONT.bmp`, role `font`, with its own
size and SHA-1. Font content changes are detected even if size/mtime are unchanged.

After changing the font or sidecar, run a targeted scan of the ROM and refresh the
client manifest/package. ROM SHA-1 alone cannot detect font changes. Other language
versions do not inherit this association. The server does not supply fonts, and a
successful download does not prove correct client rendering.
