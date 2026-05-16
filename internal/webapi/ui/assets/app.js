// nimbusfab UI Phase 2 — vanilla JS for browser-triggered deployments
// with live log streaming via EventSource. No framework, no build step.
(function () {
  "use strict";

  function attachDeploymentActions(deploymentID) {
    const log = document.getElementById("event-log");
    const buttons = document.querySelectorAll("[data-action]");
    const apiToken = document.currentScript ? document.currentScript.dataset.apiToken : "";

    function authHeaders() {
      return apiToken ? { Authorization: "Bearer " + apiToken } : {};
    }

    function appendLog(html, cls) {
      const div = document.createElement("div");
      div.className = "log-line " + (cls || "");
      div.innerHTML = html;
      log.appendChild(div);
      log.scrollTop = log.scrollHeight;
    }

    function setBusy(busy) {
      buttons.forEach((b) => (b.disabled = busy));
    }

    function startStream() {
      const src = new EventSource("/api/v1/deployments/" + encodeURIComponent(deploymentID) + "/events");
      ["start", "log", "progress", "success", "failure", "diagnostic", "skip", "terminal"].forEach((kind) => {
        src.addEventListener(kind, (e) => appendLog(formatEvt(kind, e.data), kind));
      });
      src.addEventListener("complete", () => {
        appendLog("<em>operation complete</em>", "complete");
        src.close();
        setBusy(false);
        // Full reload so target statuses re-read from inventory.
        setTimeout(() => window.location.reload(), 500);
      });
      src.onerror = () => {
        appendLog("<em>connection error</em>", "failure");
        src.close();
        setBusy(false);
      };
      return src;
    }

    function trigger(op) {
      if (op === "destroys" && !confirm("Destroy this deployment? This is not reversible.")) return;
      setBusy(true);
      log.innerHTML = "";
      appendLog("<em>kicking off " + escapeHtml(op) + "…</em>");
      const src = startStream();
      fetch("/api/v1/deployments/" + encodeURIComponent(deploymentID) + "/" + op, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: "{}",
      })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
          return r.json();
        })
        .catch((err) => {
          appendLog("<em>POST failed: " + escapeHtml(err.message) + "</em>", "failure");
          setBusy(false);
          src.close();
        });
    }

    buttons.forEach((b) => {
      b.addEventListener("click", () => trigger(b.dataset.action));
    });
  }

  function formatEvt(kind, raw) {
    try {
      const o = JSON.parse(raw);
      const target = o.cloud && o.region ? o.cloud + "/" + o.region : "";
      return (
        '<span class="ts">' + escapeHtml(o.timestamp || "") + "</span> " +
        '<span class="target">' + escapeHtml(target) + "</span> " +
        '<span class="kind">' + escapeHtml(kind) + "</span> " +
        '<span class="msg">' + escapeHtml(o.message || "") + "</span>"
      );
    } catch (e) {
      return escapeHtml(raw);
    }
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      "\"": "&quot;",
      "'": "&#39;",
    }[c]));
  }

  window.nimbusfab = { attachDeploymentActions: attachDeploymentActions };
})();
