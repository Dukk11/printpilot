// PrintPilot dashboard: polls the /fragment endpoint and swaps the card grid.
// No framework, no build step — this file is the whole frontend logic.
(function () {
  "use strict";

  var grid = document.getElementById("grid");
  if (!grid) return;

  var POLL_MS = 2000;
  var JITTER_MS = 400;
  var timer = null;

  function refresh() {
    // fetch with cache-buster; keep it simple and forgiving.
    fetch("/fragment?_=" + Date.now(), { cache: "no-store" })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.text();
      })
      .then(function (html) {
        grid.innerHTML = html;
        document.body.dataset.stale = "false";
      })
      .catch(function () {
        // Keep showing the last known state; mark stale so a future CSS
        // hook can surface it. Retry happens on the next tick anyway.
        document.body.dataset.stale = "true";
      })
      .then(function () {
        schedule();
      });
  }

  function jitter() {
    if (window.crypto && crypto.getRandomValues) {
      var arr = new Uint16Array(1);
      crypto.getRandomValues(arr);
      return arr[0] % JITTER_MS;
    }
    return Date.now() % JITTER_MS;
  }

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, POLL_MS + jitter());
  }

  // Pause polling while the tab is hidden; resume immediately on focus.
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      clearTimeout(timer);
    } else {
      refresh();
    }
  });

  schedule();
})();
