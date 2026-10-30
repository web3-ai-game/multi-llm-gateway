// ── multi-llm-chat 设置页：Cherry 风格助手管理 + 改密 ──────────────────────────────
(function () {
  const el = (id) => document.getElementById(id);
  let vendors = [];
  let editingId = null; // null=新建

  let editingAssistant = null;

  // ── assistants ──
  async function loadAssistants() {
    const list = (await DW.api("/api/assistants")) || [];
    const box = el("assistantList");
    box.innerHTML = "";
    list.forEach((a) => {
      const card = document.createElement("div");
      card.className = "assistant-card";
      card.innerHTML =
        `<div class="a-emoji"><svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><path d="M12 8v8M8 12h8"></path></svg></div>` +
        `<div class="a-name">${DW.escapeHTML(a.name)}</div>` +
        (a.system_prompt ? `<div class="a-prompt">${DW.escapeHTML(a.system_prompt)}</div>` : "") +
        (a.is_default ? `<span class="tag admin a-default" data-i18n="settings.default">${DW.t("settings.default")}</span>` : "");
      card.onclick = () => openModal(a);
      box.appendChild(card);
    });
    const add = document.createElement("div");
    add.className = "assistant-card add-card";
    add.textContent = "＋ " + DW.t("settings.new.assistant");
    add.onclick = () => openModal(null);
    box.appendChild(add);
  }

  let templates = [];

  // ── modal ──
  async function renderTemplates() {
    const box = el("tplChips");
    if (!box) return;
    if (templates.length === 0) {
      templates = (await DW.api("/api/assistant-templates")) || [];
    }
    box.innerHTML = "";
    templates.forEach((tpl) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "preset-btn";
      btn.style.fontSize = "12px";
      btn.style.padding = "4px 8px";
      btn.textContent = tpl.name;
      btn.onclick = () => {
        el("aName").value = tpl.name;
        el("aPrompt").value = tpl.system_prompt;
        el("aTemp").value = tpl.temperature || 0.7;
        el("aTempVal").textContent = Number(tpl.temperature || 0.7).toFixed(1);
      };
      box.appendChild(btn);
    });
  }

  function openModal(a) {
    editingId = a ? a.id : null;
    editingAssistant = a || null;
    el("modalTitle").textContent = DW.t(a ? "settings.edit.assistant" : "settings.new.assistant");
    el("templateSection").style.display = a ? "none" : "";
    el("aName").value = a ? a.name : "";
    el("aPrompt").value = a ? a.system_prompt : "";
    const temp = a ? a.temperature : 0.7;
    el("aTemp").value = temp;
    el("aTempVal").textContent = Number(temp).toFixed(1);
    // 默认助手不可删除
    el("aDelete").style.display = a && !a.is_default ? "" : "none";
    if (!a) renderTemplates();
    el("modal").classList.add("show");
  }
  function closeModal() { el("modal").classList.remove("show"); editingId = null; editingAssistant = null; }

  async function saveAssistant() {
    const body = {
      name: el("aName").value.trim(),
      emoji: "AI",
      system_prompt: el("aPrompt").value,
      model_id: (editingAssistant && editingAssistant.model_id) || "supergrok",
      temperature: parseFloat(el("aTemp").value),
    };
    if (!body.name) {
      el("aName").focus();
      return;
    }
    try {
      if (editingId) {
        await DW.api("/api/assistants/" + editingId, { method: "PUT", body: JSON.stringify(body) });
      } else {
        await DW.api("/api/assistants", { method: "POST", body: JSON.stringify(body) });
      }
      closeModal();
      await loadAssistants();
    } catch (e) {
      const errBox = el("modalErrMsg");
      if (errBox) { errBox.textContent = e.message || DW.t("common.error"); errBox.style.display = ""; }
    }
  }

  async function deleteAssistant() {
    if (!editingId) return;
    try {
      await DW.api("/api/assistants/" + editingId, { method: "DELETE" });
      closeModal();
      await loadAssistants();
    } catch (e) {
      const errBox = el("modalErrMsg");
      if (errBox) { errBox.textContent = e.message || DW.t("common.error"); errBox.style.display = ""; }
    }
  }

  // ── password ──
  async function changePassword(e) {
    e.preventDefault();
    const msg = el("pwdMsg");
    msg.style.color = "";
    const oldp = el("oldPwd").value, newp = el("newPwd").value;
    try {
      await DW.api("/api/account/password", { method: "POST", body: JSON.stringify({ old_password: oldp, new_password: newp }) });
      msg.style.color = "#4fae74";
      msg.textContent = DW.t("settings.pwd.ok");
      el("oldPwd").value = ""; el("newPwd").value = "";
    } catch (e2) {
      msg.style.color = "#e0674f";
      msg.textContent = DW.t("settings.pwd.err");
    }
  }

  // ── 套餐概览（顶部充值入口）──
  async function loadPlan() {
    try {
      const d = await DW.api("/api/billing/info");
      const key = d.plan === "pro" ? "bill.plan.pro" : d.plan === "unlimited" ? "bill.plan.unlimited" : "bill.plan.free";
      el("sPlanName").textContent = DW.t(key);
      el("sProTag").style.display = d.is_pro ? "" : "none";
      el("sToday").textContent = d.today_messages || 0;
      const target = new Date(d.next_reset).getTime();
      const tick = () => {
        const ms = target - Date.now();
        const c = el("sCountdown");
        if (!c) return;
        if (ms <= 0) { c.textContent = "00:00:00"; return; }
        const h = Math.floor(ms / 3600000), m = Math.floor((ms % 3600000) / 60000), s = Math.floor((ms % 60000) / 1000);
        c.textContent = [h, m, s].map((n) => String(n).padStart(2, "0")).join(":");
      };
      tick();
      setInterval(tick, 1000);
    } catch (e) {}
  }

  async function init() {
    loadPlan();
    await loadAssistants();
    el("aTemp").addEventListener("input", () => { el("aTempVal").textContent = Number(el("aTemp").value).toFixed(1); });
    el("aCancel").onclick = closeModal;
    el("aSave").onclick = saveAssistant;
    el("aDelete").onclick = deleteAssistant;
    el("modal").addEventListener("click", (e) => { if (e.target === el("modal")) closeModal(); });
    el("pwdForm").addEventListener("submit", changePassword);
  }

  document.addEventListener("dw:ready", init);
  document.addEventListener("dw:langchange", () => { loadAssistants(); });
})();
