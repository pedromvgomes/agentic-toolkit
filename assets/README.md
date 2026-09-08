# Brand assets

The mark `agtk` posts reviews under. `agtk code-review run --pr N` posts as a GitHub App,
and this is the App's face on every review it leaves.

| File | What it is |
|---|---|
| `agtk-bot-mark.svg` | The mark itself, on no background. Source of truth; everything else is rendered from it. |
| `agtk-bot-logo.png` | 1024×1024, what GitHub's App settings page takes. |

## Why the logo is not just the mark

The mark is near-black. GitHub renders a bot's avatar on the page background, which on a dark
theme is also near-black, so the mark on its own disappears into the chrome exactly where a
review is read. The white plate is what keeps it legible in both themes.

The plate's corners are transparent and its radius is 22.5%, which is the app-icon shape. A
square plate would read as a white tile wherever the logo is shown uncropped.

## Regenerating the logo

```sh
make logo
```

The plate is injected ahead of the artwork at render time rather than living in a second SVG,
so there is one copy of the paths and no way for the two to drift. Needs `rsvg-convert`
(`brew install librsvg`).

## Palette

| | |
|---|---|
| Ink — head, ears, tail | `#1D242A` |
| Visor, ear slots | `#FDFDFD` |
| Plate | `#FFFFFF` |
| Left chevron | `#1AD082` → `#0AB6B2` |
| Right chevron | `#17C88C` → `#08B0B5` |
| Mouth bar | `#1FDE82` → `#04BAD3` |
| Status bar | `#707175` |
