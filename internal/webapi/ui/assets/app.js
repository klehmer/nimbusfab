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

  function attachGraph() {
    document.querySelectorAll('.graph-toolbar .seg').forEach((btn) => {
      btn.addEventListener('click', () => {
        const dir = btn.dataset.dir;
        document.cookie = 'nf_graph_dir=' + dir + '; path=/; max-age=31536000; samesite=lax';
        const url = new URL(window.location.href);
        url.searchParams.set('dir', dir);
        window.location.href = url.toString();
      });
    });

    const canvas = document.querySelector('.graph-canvas');
    if (!canvas) return;
    const targets = JSON.parse(canvas.dataset.targetsJson || '{}');
    const components = JSON.parse(canvas.dataset.componentsJson || '{}');

    document.querySelectorAll('svg .graph-node').forEach((node) => {
      node.addEventListener('click', () => {
        const name = node.dataset.component;
        renderNodeDetail(name, components[name] || null, targets[name] || []);
      });
    });

    const closeBtn = document.getElementById('node-detail-close');
    if (closeBtn) {
      closeBtn.addEventListener('click', () => {
        document.getElementById('node-detail').hidden = true;
      });
    }
  }

  function renderNodeDetail(name, component, targetList) {
    const panel = document.getElementById('node-detail');
    if (!panel) return;
    document.getElementById('node-detail-title').textContent = name;

    const typeEl = document.getElementById('node-detail-type');
    typeEl.textContent = component && component.type ? component.type : '';

    const specTable = document.getElementById('node-detail-spec');
    const specBody = specTable.querySelector('tbody');
    specBody.innerHTML = '';
    const spec = (component && component.spec) || {};
    const specKeys = Object.keys(spec).sort();
    if (specKeys.length === 0) {
      specTable.hidden = true;
    } else {
      specKeys.forEach((k) => {
        const row = document.createElement('tr');
        const keyCell = document.createElement('th');
        keyCell.textContent = k;
        const valCell = document.createElement('td');
        valCell.textContent = formatSpecValue(spec[k]);
        row.appendChild(keyCell);
        row.appendChild(valCell);
        specBody.appendChild(row);
      });
      specTable.hidden = false;
    }

    const ul = document.getElementById('node-detail-targets');
    ul.innerHTML = '';
    if (targetList.length === 0) {
      const li = document.createElement('li');
      li.className = 'muted';
      li.textContent = 'No targets yet';
      ul.appendChild(li);
    } else {
      targetList.forEach((t) => {
        const li = document.createElement('li');
        const cloud = document.createElement('span');
        cloud.className = 'badge';
        cloud.textContent = t.cloud;
        const region = document.createElement('code');
        region.textContent = ' ' + t.region + ' ';
        const status = document.createElement('span');
        status.className = 'badge status-' + (t.status || 'unknown');
        status.textContent = t.status || 'unknown';
        li.appendChild(cloud);
        li.appendChild(region);
        li.appendChild(status);
        ul.appendChild(li);
      });
    }
    panel.hidden = false;
  }

  function formatSpecValue(v) {
    if (v === null || v === undefined) return '—';
    if (typeof v === 'boolean') return v ? 'yes' : 'no';
    if (typeof v === 'object') return JSON.stringify(v);
    return String(v);
  }

  window.nimbusfab = { attachDeploymentActions: attachDeploymentActions, attachGraph: attachGraph };
})();
