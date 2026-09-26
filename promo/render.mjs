#!/usr/bin/env node
// Renders the promo deterministically: Playwright captures each frame by
// calling window.seek(t); ffmpeg encodes MP4 and gifski encodes GIF.
//
//   node render.mjs                 # all variants
//   node render.mjs landscape-full  # one variant
//   node render.mjs --stills        # one PNG per scene for review
//   node render.mjs --cards         # link-preview images (GitHub + Open Graph)
import { chromium } from "playwright";
import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
const FPS = 30;
const GIF_FPS = 15;

const VARIANTS = {
  "landscape-full": { layout: "landscape", cut: "full", out: "sb-heartbeat-promo", gifWidth: 1200, video: "1920:1080" },
  "landscape-short": { layout: "landscape", cut: "short", out: "sb-heartbeat-promo-short", gifWidth: 1200, video: "1920:1080" },
  "square-full": { layout: "square", cut: "full", out: "sb-heartbeat-promo-square", gifWidth: 1080, video: "1080:1080" },
  "square-short": { layout: "square", cut: "short", out: "sb-heartbeat-promo-square-short", gifWidth: 1080, video: "1080:1080" },
};

// Static images from src/card.html. `variant` selects the layout; `transparent`
// keeps the PNG background clear.
const CARDS = {
  "sb-heartbeat-github-social.png": { width: 1280, height: 640 },
  "sb-heartbeat-og.png": { width: 1200, height: 630 },
  "sb-heartbeat-title.png": { width: 1920, height: 1080, variant: "title" },
  "sb-heartbeat-title-square.png": { width: 1080, height: 1080, variant: "title" },
  "sb-heartbeat-logo.png": { width: 1200, height: 300, variant: "logo", transparent: true },
};

const args = process.argv.slice(2);
const stills = args.includes("--stills");
const cards = args.includes("--cards");
const selected = args.filter((a) => !a.startsWith("--"));
for (const name of selected) {
  if (!VARIANTS[name]) {
    console.error(`Unknown variant "${name}". Choose from: ${Object.keys(VARIANTS).join(", ")}`);
    process.exit(2);
  }
}

// Only video/GIF rendering needs the encoders.
if (!stills && !cards) {
  for (const tool of ["ffmpeg", "gifski"]) {
    try {
      execFileSync("which", [tool], { stdio: "ignore" });
    } catch {
      console.error(`Missing required tool: ${tool}`);
      process.exit(1);
    }
  }
}

const browser = await chromium.launch();
try {
  if (stills) {
    await renderStills(selected[0] ?? "landscape-full");
  } else if (cards) {
    await renderCards();
  } else {
    for (const name of selected.length ? selected : Object.keys(VARIANTS)) {
      await renderVariant(name);
    }
  }
} finally {
  await browser.close();
}

// Frames are captured at 2x so the MP4 can be a crisp 1920x1080 and the GIF is
// downscaled (with better anti-aliasing) by gifski.
async function openPage({ layout, cut }, scale = 1) {
  const size = layout === "square" ? { width: 1080, height: 1080 } : { width: 1200, height: 675 };
  const page = await browser.newPage({ viewport: size, deviceScaleFactor: scale });
  const url = pathToFileURL(join(root, "src", "index.html"));
  url.search = new URLSearchParams({ layout, cut, render: "1" }).toString();
  await page.goto(url.href);
  await page.evaluate(() => document.fonts.ready);
  const fonts = await page.evaluate(() => [...document.fonts].filter((f) => f.status === "loaded").length);
  if (fonts < 5) throw new Error(`Expected 5 embedded fonts to load, got ${fonts}. Run npm install in promo/.`);
  return page;
}

async function renderCards() {
  const dist = join(root, "dist");
  mkdirSync(dist, { recursive: true });
  for (const [file, { width, height, variant, transparent }] of Object.entries(CARDS)) {
    const page = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 1 });
    const url = pathToFileURL(join(root, "src", "card.html"));
    url.search = new URLSearchParams({ w: String(width), h: String(height), variant: variant ?? "" }).toString();
    await page.goto(url.href);
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: join(dist, file), omitBackground: Boolean(transparent) });
    await page.close();
    console.log(`${file}: ${width}x${height}`);
  }
}

async function renderStills(name) {
  const page = await openPage(VARIANTS[name]);
  const dir = join(root, "stills", name);
  mkdirSync(dir, { recursive: true });
  // Capture each scene just before the next one wipes in, using the page's
  // own timing so stills stay correct for every cut. The opening scene also
  // gets a frame before its pulse.
  const { scenes, openSweep, loop } = await page.evaluate(() => window.PROMO);
  const shots = [["00-flatline", openSweep - 0.5]];
  scenes.forEach(([id], i) => {
    const next = i + 1 < scenes.length ? scenes[i + 1][1] : loop;
    shots.push([`${String(i + 1).padStart(2, "0")}-${id.slice(2)}`, next - 0.2]);
  });
  for (const [label, t] of shots) {
    await page.evaluate((time) => window.seek(time), t);
    await page.screenshot({ path: join(dir, `${label}.png`) });
  }
  console.log(`Stills written to ${dir}`);
}

async function renderVariant(name) {
  const variant = VARIANTS[name];
  const page = await openPage(variant, 2);
  const { duration } = await page.evaluate(() => window.PROMO);
  const frames = Math.round(duration * FPS);
  const frameDir = join(root, ".frames", name);
  rmSync(frameDir, { recursive: true, force: true });
  mkdirSync(frameDir, { recursive: true });

  for (let i = 0; i < frames; i++) {
    await page.evaluate((time) => window.seek(time), i / FPS);
    await page.screenshot({ path: join(frameDir, `f${String(i).padStart(5, "0")}.png`) });
  }
  await page.close();

  const dist = join(root, "dist");
  mkdirSync(dist, { recursive: true });
  const mp4 = join(dist, `${variant.out}.mp4`);
  const gif = join(dist, `${variant.out}.gif`);

  execFileSync("ffmpeg", [
    "-y", "-loglevel", "error",
    "-framerate", String(FPS), "-i", join(frameDir, "f%05d.png"),
    "-vf", `scale=${variant.video}:flags=lanczos`,
    "-c:v", "libx264", "-preset", "slow", "-crf", "20",
    "-pix_fmt", "yuv420p", "-movflags", "+faststart",
    mp4,
  ]);

  // Every second frame gives 15 fps for the GIF.
  const gifFrames = [];
  for (let i = 0; i < frames; i += FPS / GIF_FPS) gifFrames.push(join(frameDir, `f${String(i).padStart(5, "0")}.png`));
  execFileSync("gifski", [
    "--quiet", "--fps", String(GIF_FPS), "--width", String(variant.gifWidth),
    "--quality", "80", "-o", gif, ...gifFrames,
  ]);

  const mb = (p) => (statSync(p).size / 1024 / 1024).toFixed(2);
  console.log(`${name}: ${frames} frames, ${duration.toFixed(1)}s → ${variant.out}.mp4 (${mb(mp4)} MB), ${variant.out}.gif (${mb(gif)} MB)`);
}
