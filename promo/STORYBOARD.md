# SB Heartbeat promo — storyboard

Status: **built**. See [README.md](README.md) for rendering.

## Decisions

- Silent: all meaning is carried by on-screen text.
- GIF-safe: flat palette, no gradients or blur, seamless loop.
- High-level features only; the best-effort disclaimer lives in the project
  README, not the promo.
- Install line uses `@latest` so the asset does not go stale each release
  (owner decision). The README and `llms.txt` keep pinned, checksum-verified
  installs as the documented path.
- The opening shows an idle (not paused) project: a heartbeat keeps an active
  project active; it cannot resume a paused one.
- Variants: 16:9 and 1:1, each in a full (~29 s) and short (~18 s) cut.
- No Supabase logo or third-party marks; platforms appear as text.

## Motif

An ECG line. The quiet project starts flatlined ("Idle"), one pulse brings it to
"Active", and each subsequent pulse sweeps in the next scene. The final sweep is
a flat line that redraws the opening frame, so the loop is invisible.

## Scenes (full cut)

| # | Start | Scene | On-screen copy |
| --- | --- | --- | --- |
| 1 | 0.0 s | Flatline, project card **Idle** | "Quiet Supabase project?" |
| 2 | 2.2 s | Pulse → **Active**, logotype | "Don’t let it flatline." |
| 3 | 5.2 s | Week strip, three green dots per day | "Keeps low-traffic projects active" |
| 4 | 9.0 s | `GET /rest/v1/sb_heartbeat?…` → `200 [{"id":true}]` | "One tiny, read-only query" · low-privilege key only · no writes · no database password |
| 5 | 13.2 s | Six platform cards light up | "Runs wherever you already run things" · GitHub Actions, Cloudflare Workers, Docker, cron, launchd, systemd |
| 6 | 17.0 s | Chat: "Add sb-heartbeat to this repo." → agent checklist | "Ready for coding agents" · `llms.txt`, checksum-verified release, SQL for review, `doctor` |
| 7 | 21.0 s | Feature chips | "And more" · diagnostics, JSON, Prometheus, webhooks, history, multi-project, exit codes, MIT |
| 8 | 24.4 s | End card | "A tiny pulse for quiet Supabase projects." · `go install …@latest` · repo URL |
| — | 28.4 s | Flat line sweeps back to scene 1 | (loop) |

The short cut keeps scenes 1–2, 4, 5, 6, and 8.

## Tagline options

Current headline: **"Don’t let it flatline."** End card: **"A tiny pulse for
quiet Supabase projects."** Alternatives:

- "Keep your Supabase projects breathing."
- "A pulse, not a pager."
- "Your side project’s pacemaker."

Avoid anything that promises projects will never pause; activity rules are
Supabase's to change.
