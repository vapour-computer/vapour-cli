// Vapour local test overlay. Injected by `vapour test` only; never part of a published build.
// Measures frame times with requestAnimationFrame. Nothing leaves this machine except a
// summary posted back to the local `vapour test` process when the page is closed.
(function () {
  "use strict";
  var MAX = 1200; // ring of recent frame times (about 20s at 60fps)
  var times = new Float32Array(MAX);
  var recent = [];
  var n = 0, total = 0, worst = 0, sumMs = 0;
  var last = 0, winStart = 0, winFrames = 0, fps = 0;
  var start = 0, lastFlush = 0;
  var batchFrames = [];

  var box = document.createElement("div");
  box.setAttribute("data-vapour-overlay", "");
  box.style.cssText =
    "position:fixed;top:8px;left:8px;z-index:2147483647;pointer-events:none;" +
    "font:12px/1.4 ui-monospace,monospace;color:#fff;background:rgba(0,0,0,.65);" +
    "padding:6px 8px;border-radius:6px;white-space:pre;";
  var visible = true;
  function mount() { (document.body || document.documentElement).appendChild(box); }
  if (document.body) mount(); else addEventListener("DOMContentLoaded", mount);

  addEventListener("keydown", function (e) {
    if (e.ctrlKey && e.shiftKey && e.code === "KeyF") {
      visible = !visible;
      box.style.display = visible ? "" : "none";
    }
  });

  function low1() {
    var len = Math.min(n, MAX);
    if (len < 20) return 0;
    var a = Array.prototype.slice.call(times, 0, len).sort(function (x, y) { return y - x; });
    var k = Math.max(1, Math.floor(len * 0.01));
    var s = 0;
    for (var i = 0; i < k; i++) s += a[i];
    return 1000 / (s / k);
  }

  function median(values) {
    if (!values.length) return 0;
    var a = values.slice().sort(function (x, y) { return x - y; });
    return a[(a.length / 2) | 0];
  }

  function sendBatch(now) {
    if (!batchFrames.length) return;
    var payload = JSON.stringify({
      frames: batchFrames
    });
    try { navigator.sendBeacon("/__vapour/records", payload); } catch (e) {}
    batchFrames = [];
    lastFlush = now;
  }

  function frame(t) {
    if (last) {
      var d = t - last;
      if (!start) start = t;
      times[n % MAX] = d;
      n++; total++; sumMs += d;
      if (d > worst) worst = d;
      var spike = false;
      if (recent.length >= 10) {
        var m = median(recent);
        spike = m > 0 && d > m * 2;
      }
      recent.push(d);
      if (recent.length > 120) recent.shift();
      batchFrames.push({ durationMs: d, timeMs: t - start, spike: spike, epochMs: performance.timeOrigin + t });
      winFrames++;
      if (!lastFlush) lastFlush = t;
      if (t - lastFlush >= 1000) sendBatch(t);
      if (t - winStart >= 500) {
        fps = (winFrames * 1000) / (t - winStart);
        winStart = t; winFrames = 0;
        if (visible) {
          box.textContent =
            "FPS " + fps.toFixed(0) + "\n" +
            "frame " + d.toFixed(1) + " ms\n" +
            "1% low " + low1().toFixed(0) + " fps\n" +
            "worst " + worst.toFixed(0) + " ms";
        }
      }
    } else {
      winStart = t;
      start = t;
    }
    last = t;
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);

  var sent = false;
  function report() {
    if (sent || total < 2) return;
    sent = true;
    sendBatch(performance.now());
    var body = JSON.stringify({
      frames: total,
      avgFps: total ? (total * 1000) / sumMs : 0,
      low1Fps: low1(),
      worstMs: worst
    });
    try { navigator.sendBeacon("/__vapour/report", body); } catch (e) {}
  }
  addEventListener("pagehide", report);
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") { sent = false; report(); }
  });
})();
