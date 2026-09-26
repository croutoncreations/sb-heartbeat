// Deterministic timeline: every frame is a pure function of time `t` (seconds).
// render.mjs calls window.seek(t) for each frame; opening index.html directly
// plays the animation in real time for previewing.
(() => {
  const params = new URLSearchParams(location.search);
  const layout = params.get("layout") === "square" ? "square" : "landscape";
  const cut = params.get("cut") === "short" ? "short" : "full";
  document.body.classList.add(layout);

  const W = layout === "square" ? 1080 : 1200;
  const H = layout === "square" ? 1080 : 675;
  const MID = H / 2;
  const WIPE = 0.9; // seconds for an ECG sweep transition
  const TRAIL = W * 0.35; // length of the green trace behind the sweep head
  const AMP = layout === "square" ? 100 : 80;
  const PERIOD = layout === "square" ? 270 : 300;

  // Scene start times. Each scene is wiped in at `start` and wiped out when the
  // next one starts. The final "loop" wipe returns to the exact t=0 frame.
  const CUTS = {
    full: {
      openSweep: 2.2,
      scenes: [
        ["s-open", 0],
        ["s-active", 5.2],
        ["s-query", 9.0],
        ["s-anywhere", 13.2],
        ["s-agents", 17.0],
        ["s-more", 21.0],
        ["s-end", 24.4],
      ],
      loop: 28.4,
    },
    short: {
      openSweep: 1.7,
      scenes: [
        ["s-open", 0],
        ["s-query", 4.4],
        ["s-anywhere", 7.9],
        ["s-agents", 11.1],
        ["s-end", 14.6],
      ],
      loop: 17.6,
    },
  };
  const plan = CUTS[cut];
  const SWEEP_DUR = 1.6;
  const DURATION = plan.loop + WIPE;

  const $ = (id) => document.getElementById(id);
  const clamp = (v, lo = 0, hi = 1) => Math.min(hi, Math.max(lo, v));
  const easeOut = (p) => 1 - Math.pow(1 - p, 3);

  // ---------- ECG geometry ----------
  // Piecewise-linear PQRST complex, (dx, dy) in units of AMP; negative dy = up.
  const COMPLEX = [
    [-70, 0], [-58, -0.1], [-46, 0], [-16, 0], [-10, 0.14], [0, -1],
    [9, 0.5], [16, 0], [42, 0], [58, -0.18], [74, 0],
  ];
  function aliveY(x) {
    const local = ((x % PERIOD) + PERIOD) % PERIOD - PERIOD / 2;
    for (let i = 0; i < COMPLEX.length - 1; i++) {
      const [x0, y0] = COMPLEX[i];
      const [x1, y1] = COMPLEX[i + 1];
      if (local >= x0 && local <= x1) {
        return MID + AMP * (y0 + ((local - x0) / (x1 - x0)) * (y1 - y0));
      }
    }
    return MID;
  }
  function segment(x0, x1, alive) {
    x0 = Math.max(0, x0);
    x1 = Math.min(W, x1);
    if (x1 <= x0) return "";
    if (!alive) return `M${x0} ${MID}H${x1}`;
    let d = `M${x0.toFixed(1)} ${aliveY(x0).toFixed(1)}`;
    for (let x = Math.ceil(x0); x <= x1; x += 1) d += `L${x} ${aliveY(x).toFixed(1)}`;
    return d;
  }

  // ---------- helpers ----------
  function show(el, lt, at, dur = 0.35, dist = 14) {
    const p = easeOut(clamp((lt - at) / dur));
    el.style.opacity = p;
    el.style.transform = `translateY(${(1 - p) * dist}px)`;
  }
  function typed(el, text, lt, at, cps) {
    const n = Math.floor(clamp((lt - at) * cps, 0, text.length));
    el.textContent = text.slice(0, n);
    return n === text.length;
  }
  function headX(t, start) {
    return ((t - start) / WIPE) * (W + TRAIL);
  }

  // ---------- one-time DOM setup ----------
  const days = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
  const week = $("week");
  const shownDays = layout === "square" ? days.slice(0, 5) : days;
  for (const label of shownDays) {
    const day = document.createElement("div");
    day.className = "day";
    day.innerHTML = `<div class="label">${label}</div><div class="dots"><i></i><i></i><i></i></div><div class="state">active</div>`;
    week.appendChild(day);
  }
  const REQ = "/rest/v1/sb_heartbeat?select=id&id=eq.true&limit=1";
  const INSTALL = "go install github.com/croutoncreations/sb-heartbeat/cmd/sb-heartbeat@latest";
  const ecgBase = $("ecg-base");
  const ecgBeat = $("ecg-beat");
  $("ecg").setAttribute("viewBox", `0 0 ${W} ${H}`);

  // ---------- per-scene renderers (lt = local time since scene start) ----------
  const renderers = {
    "s-open"(lt) {
      const sweepHead = ((lt - plan.openSweep) / SWEEP_DUR) * W;
      const flipped = sweepHead >= W / 2;
      $("open-badge").classList.toggle("active", flipped);
      $("open-badge").querySelector("b").textContent = flipped ? "Active" : "Idle";
      for (const s of $("open-card").querySelectorAll(".db-icon span")) {
        s.style.background = flipped ? "var(--green)" : "var(--slate)";
      }
      // The sweep head erases the caption; the title follows behind the trace.
      const x = clamp(sweepHead, 0, W);
      const reveal = clamp(sweepHead - TRAIL * 0.6, 0, W);
      $("open-caption").style.clipPath = `inset(0 0 0 ${x}px)`;
      const title = $("open-title");
      title.style.opacity = reveal > 0 ? 1 : 0;
      title.style.clipPath = `inset(0 ${W - reveal}px 0 0)`;
    },
    "s-active"(lt) {
      const dots = week.querySelectorAll(".dots i");
      dots.forEach((dot, i) => dot.classList.toggle("on", lt >= 0.7 + i * 0.09));
      week.querySelectorAll(".day").forEach((day, i) => {
        show(day, lt, 0.35 + i * 0.05, 0.3);
        day.querySelector(".state").style.opacity = lt >= 0.7 + (i * 3 + 2) * 0.09 ? 1 : 0;
      });
    },
    "s-query"(lt) {
      show($("req"), lt, 0.3, 0.3);
      const done = typed($("req-text"), REQ, lt, 0.45, 45);
      $("req-cursor").style.opacity = done ? 0 : 1;
      const typedEnd = 0.45 + REQ.length / 45;
      show($("req-arrow"), lt, typedEnd + 0.05, 0.25, 0);
      show($("res"), lt, typedEnd + 0.25, 0.3);
      [...$("query-ticks").children].forEach((li, i) => show(li, lt, typedEnd + 0.6 + i * 0.2, 0.3));
    },
    "s-anywhere"(lt) {
      [...$("targets").children].forEach((el, i) => {
        const at = 0.4 + i * 0.14;
        show(el, lt, at, 0.3);
        el.classList.toggle("lit", lt >= at && lt < at + 0.6);
      });
    },
    "s-agents"(lt) {
      show($("chat-user"), lt, 0.35, 0.3);
      [...$("agent-steps").children].forEach((li, i) => show(li, lt, 1.0 + i * 0.45, 0.3));
      show($("agent-steps"), lt, 0.85, 0.3);
    },
    "s-more"(lt) {
      [...$("chips").children].forEach((el, i) => show(el, lt, 0.35 + i * 0.1, 0.3));
    },
    "s-end"(lt) {
      const end = $("s-end");
      show(end.querySelector(".logotype"), lt, 0.25, 0.4);
      show(end.querySelector(".tagline"), lt, 0.55, 0.4);
      show($("install"), lt, 0.9, 0.3);
      typed($("install-text"), INSTALL, lt, 1.05, 70);
      show(end.querySelector(".repo"), lt, 1.05 + INSTALL.length / 70 + 0.1, 0.3);
    },
  };

  // Reset every scene, then paint only the ones visible at time t.
  function seek(t) {
    t = ((t % DURATION) + DURATION) % DURATION;
    const scenes = plan.scenes;
    for (const [id] of scenes) {
      const el = $(id);
      el.style.opacity = 0;
      el.style.clipPath = "none";
    }
    let base = "";
    let beat = "";

    const sweepStart = plan.openSweep;
    const openSwept = t >= sweepStart + SWEEP_DUR;
    const openAlive = t >= sweepStart;

    for (let i = 0; i < scenes.length; i++) {
      const [id, start] = scenes[i];
      const next = i + 1 < scenes.length ? scenes[i + 1][1] : plan.loop;
      if (t < start || t >= next + WIPE) continue;
      const el = $(id);
      el.style.opacity = 1;
      // Incoming scenes are revealed only behind the green trace, leaving a
      // clean gap between the outgoing and incoming content.
      if (i > 0 && t < start + WIPE) {
        el.style.clipPath = `inset(0 ${W - clamp(headX(t, start) - TRAIL, 0, W)}px 0 0)`;
      } else if (t >= next) {
        el.style.clipPath = `inset(0 0 0 ${clamp(headX(t, next), 0, W)}px)`;
      }
      renderers[id](t - start);
    }

    // ECG trace for the opening scene.
    const firstNext = scenes[1][1];
    if (t < firstNext + WIPE) {
      const cutoff = t >= firstNext ? clamp(headX(t, firstNext), 0, W) : 0;
      if (!openAlive) {
        base += segment(cutoff, W, false);
      } else if (!openSwept) {
        const h = ((t - sweepStart) / SWEEP_DUR) * W;
        beat += segment(0, h, true);
        base += segment(Math.max(h, cutoff), W, false);
      } else {
        beat += segment(cutoff, W, true);
      }
    }
    // Sweep traces for scene-to-scene wipes.
    for (let i = 1; i < scenes.length; i++) {
      const start = scenes[i][1];
      if (t >= start && t < start + WIPE) {
        const h = headX(t, start);
        beat += segment(h - TRAIL, h, true);
      }
    }
    // Loop: a flat line redraws the opening frame exactly as it is at t=0.
    if (t >= plan.loop) {
      const h = clamp(headX(t, plan.loop), 0, W);
      const open = $("s-open");
      open.style.opacity = 1;
      open.style.clipPath = `inset(0 ${W - h}px 0 0)`;
      renderers["s-open"](0);
      base += segment(0, h, false);
    }

    ecgBase.setAttribute("d", base);
    ecgBeat.setAttribute("d", beat);
  }

  window.seek = seek;
  window.PROMO = { duration: DURATION, width: W, height: H, layout, cut, scenes: plan.scenes, openSweep: plan.openSweep, loop: plan.loop };

  if (!params.has("render")) {
    const t0 = performance.now();
    const tick = () => {
      seek((performance.now() - t0) / 1000);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  } else {
    seek(0);
  }
})();
