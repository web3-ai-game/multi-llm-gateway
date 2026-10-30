// ── Deepweay 用量看板 ──────────────────────────────────────────────────────
(function () {
  const el = (id) => document.getElementById(id);

  function fmtUSD(n) {
    if (n >= 0.01) return "$" + n.toFixed(2);
    return "$" + n.toFixed(6);
  }

  function renderModelBars(rows) {
    const box = el("byModel");
    box.innerHTML = "";
    if (!rows || rows.length === 0) {
      box.innerHTML = `<div class="empty-hint">${DW.t("dash.empty")}</div>`;
      return;
    }
    const max = Math.max(...rows.map((r) => r.spend), 0.0000001);
    rows.forEach((r) => {
      const row = document.createElement("div");
      row.className = "bar-row";
      const pct = Math.max(2, (r.spend / max) * 100);
      row.innerHTML =
        `<div class="bar-label" title="${DW.escapeHTML(r.model_name)}">${DW.escapeHTML(r.model_name)}</div>` +
        `<div class="bar-track"><div class="bar-fill" style="width:${pct}%"></div></div>` +
        `<div class="bar-val">${fmtUSD(r.spend)}</div>`;
      box.appendChild(row);
    });
  }

  function renderDayBars(rows) {
    const box = el("byDay");
    box.innerHTML = "";
    if (!rows || rows.length === 0) {
      box.innerHTML = `<div class="empty-hint">${DW.t("dash.empty")}</div>`;
      return;
    }
    const max = Math.max(...rows.map((r) => r.spend), 0.0000001);
    rows.forEach((r) => {
      const row = document.createElement("div");
      row.className = "bar-row";
      const pct = Math.max(2, (r.spend / max) * 100);
      row.innerHTML =
        `<div class="bar-label">${DW.escapeHTML(r.day)}</div>` +
        `<div class="bar-track"><div class="bar-fill" style="width:${pct}%"></div></div>` +
        `<div class="bar-val">${fmtUSD(r.spend)}</div>`;
      box.appendChild(row);
    });
  }

  async function init() {
    try {
      const me = await DW.whoami();
      if (me) {
        el("balance").textContent = me.unlimited ? DW.t("dash.unlimited") : fmtUSD(Math.max(0, me.remaining || 0));
      }
    } catch (e) {}
    try {
      const d = await DW.api("/api/usage");
      el("totalSpend").textContent = fmtUSD(d.total_spend || 0);
      el("totalMessages").textContent = d.total_messages || 0;
      renderModelBars(d.by_model);
      renderDayBars(d.by_day);
    } catch (e) {}
  }

  document.addEventListener("dw:ready", init);
})();
