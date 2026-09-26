# SB Heartbeat promo

Source for the silent motion-graphics promo. Every frame is a pure function of
time, so renders are reproducible.

## Outputs

| File | Size | Use |
| --- | --- | --- |
| `dist/sb-heartbeat-promo.gif` | 1200×675, ~29 s | GitHub README (copied to `docs/assets/`) |
| `dist/sb-heartbeat-promo.mp4` | 1920×1080, ~29 s | X, LinkedIn, Bluesky, YouTube |
| `dist/sb-heartbeat-promo-short.{gif,mp4}` | 16:9, ~18 s | Replies, short posts |
| `dist/sb-heartbeat-promo-square.{gif,mp4}` | 1080×1080, ~29 s | Instagram, Mastodon, LinkedIn feed |
| `dist/sb-heartbeat-promo-square-short.{gif,mp4}` | 1080×1080, ~18 s | Short square posts |

Every variant loops seamlessly, because the last frame is identical to the first.

## Render

Requires Node, `ffmpeg`, and `gifski`.

```bash
cd promo
npm install
npx playwright install chromium   # first time only
npm run render                    # all variants → dist/
node render.mjs landscape-full    # one variant
node render.mjs --stills          # one PNG per scene → stills/
node render.mjs --cards           # link-preview PNGs → dist/
```

After changing the landscape GIF, copy it into the README asset path:

```bash
cp dist/sb-heartbeat-promo.gif ../docs/assets/sb-heartbeat-promo.gif
```

`--cards` writes these static images to `dist/`:

| File | Size | Use |
| --- | --- | --- |
| `sb-heartbeat-github-social.png` | 1280×640 | GitHub repository social preview (Settings → Social preview) |
| `sb-heartbeat-og.png` | 1200×630 | Open Graph / link preview card |
| `sb-heartbeat-title.png` | 1920×1080 | Title card: logo + headline |
| `sb-heartbeat-title-square.png` | 1080×1080 | Square title card |
| `sb-heartbeat-logo.png` | 1200×300 | Logotype on a transparent background |

## Edit

- Text and layout: `src/index.html` and `src/styles.css`.
- Timing: `CUTS` in `src/timeline.js` (scene start times, per cut).
- Preview: open `src/index.html` in a browser. Add `?layout=square` or
  `?cut=short` to preview the other variants.

Keep the palette flat (no gradients or blur) so GIF quantisation stays clean. Use
placeholder values only; never put real project URLs or keys in the promo.
