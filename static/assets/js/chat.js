// ── Deepweay 对话页（助手 + 精选模型入口）──────────────────────────────────
(function () {
  let assistants = [];
  let sessions = [];
  let vendors = [];
  let currentSession = null;
  let streaming = false;
  let pendingModel = null;
  let lastUserMessageText = "";
  let streamAbort = null;

  const el = (id) => document.getElementById(id);
  const messagesEl = el("messages");
  const sessionListEl = el("sessionList");
  const assistantSelect = el("assistantSelect");
  const assistantChip = el("assistantChip");
  const input = el("input");
  const sendBtn = el("sendBtn");
  const sidebar = el("sidebar");
  const backdrop = el("backdrop");
  const quickPrompts = el("quickPrompts");

  let pwTimer = null;
  function showPaywall(opts) {
    const pw = el("paywall");
    if (!pw) return;
    el("pwIcon").textContent = opts.needsPro ? "🔒" : "⏳";
    el("pwTitle").textContent = DW.t(opts.needsPro ? "pw.title.pro" : "pw.title.limit");
    el("pwMsg").textContent = opts.message || "";
    const box = el("pwCountBox");
    if (opts.nextReset) {
      box.style.display = "";
      const target = new Date(opts.nextReset).getTime();
      const tick = () => {
        const ms = target - Date.now();
        const c = el("pwCountdown");
        if (!c) return;
        if (ms <= 0) { c.textContent = "00:00:00"; return; }
        const h = Math.floor(ms / 3600000), m = Math.floor((ms % 3600000) / 60000), s = Math.floor((ms % 60000) / 1000);
        c.textContent = [h, m, s].map((n) => String(n).padStart(2, "0")).join(":");
      };
      tick();
      clearInterval(pwTimer);
      pwTimer = setInterval(tick, 1000);
    } else {
      box.style.display = "none";
    }
    pw.classList.add("show");
  }
  function hidePaywall() {
    const pw = el("paywall");
    if (pw) pw.classList.remove("show");
    clearInterval(pwTimer);
  }

  async function maybeSoftNudge() {
    // 已优化：去除无故自动弹窗干扰，保持沉浸式对话体验
    try {
      const d = await DW.api("/api/billing/info");
      refreshQuota(d);
    } catch (e) {}
  }

  function refreshQuota(d) {
    const pill = el("quotaPill");
    if (!pill || !d) return;
    if (d.plan === "unlimited") {
      pill.hidden = false;
      pill.textContent = DW.t("chat.quota.unlimited");
      return;
    }
    const used = d.today_messages || 0;
    pill.hidden = false;
    pill.textContent = DW.t("chat.quota.today").replace("{n}", used);
  }

  function openSidebar() { sidebar.classList.add("open"); backdrop.classList.add("show"); }
  function closeSidebar() { sidebar.classList.remove("open"); backdrop.classList.remove("show"); }
  el("menuBtn").onclick = openSidebar;
  backdrop.onclick = closeSidebar;

  function assistantById(id) { return assistants.find((a) => a.id === id); }

  function applyVendors(list) {
    vendors = Array.isArray(list) ? list : [];
  }

  function allModels() {
    const out = [];
    vendors.forEach((v) => (v.models || []).forEach((m) => out.push(m)));
    return out;
  }
  function canonModelID(id) {
    const raw = (id || "").trim();
    if (!raw) return raw;
    const low = raw.toLowerCase();
    const stripped = low.replace(/^x-ai\//, "").replace(/^models\//, "").replace(/^google\//, "");
    const list = allModels();
    const hit = list.find((m) => {
      const mid = (m.id || "").toLowerCase();
      const rid = (m.raw_model_id || "").toLowerCase();
      return mid === low || rid === low || mid === stripped || rid === stripped || low.endsWith("/" + mid);
    });
    return hit ? hit.id : raw;
  }

  function modelMeta(id) {
    const cid = canonModelID(id);
    return allModels().find((m) => m.id === cid || m.id === id) || null;
  }
  function modelLabel(id) {
    const m = modelMeta(id);
    return (m && (m.name || m.display_name)) || id || "";
  }

  function vendorImg(vendor) {
    const v = (vendor || "").toLowerCase();
    if (v.includes("grok") || v.includes("xai") || v.includes("x-ai")) return "/img/icons/grok.png";
    if (v.includes("gemini") || v.includes("google")) return "/img/icons/gemini.png";
    if (v.includes("claude") || v.includes("anthropic")) return "/img/icons/claude.png";
    if (v.includes("gpt") || v.includes("openai")) return "/img/icons/openai.png";
    if (v.includes("deepseek")) return "/img/icons/deepseek.png";
    return "/img/icons/ai.png";
  }

  function vendorIcon(vendor) {
    const v = (vendor || "").toLowerCase();
    if (v.includes("grok") || v.includes("xai") || v.includes("x-ai")) return "𝕏";
    if (v.includes("gemini") || v.includes("google")) return "✦";
    if (v.includes("claude") || v.includes("anthropic")) return "CL";
    if (v.includes("gpt") || v.includes("openai")) return "GPT";
    if (v.includes("deepseek")) return "DS";
    if (v.includes("kimi") || v.includes("moonshot")) return "Kimi";
    if (v.includes("minimax")) return "MM";
    return "AI";
  }

  function vendorClass(vendor) {
    const v = (vendor || "").toLowerCase();
    if (v.includes("grok") || v.includes("xai") || v.includes("x-ai")) return "vendor-grok";
    if (v.includes("gemini") || v.includes("google")) return "vendor-gemini";
    if (v.includes("claude") || v.includes("anthropic")) return "vendor-claude";
    if (v.includes("gpt") || v.includes("openai")) return "vendor-gpt";
    return "vendor-default";
  }

  function modelTag(m) {
    if (!m) return "全能";
    const id = (m.id || "").toLowerCase();
    const name = (m.name || m.display_name || m.id || "").toLowerCase();
    const desc = (m.desc || m.description || "").toLowerCase();
    if (name.includes("supergrok") || id.includes("supergrok") || name.includes("4.6") || id.includes("4.6")) return "旗舰 · 限120条/时";
    if (name.includes("vision") || id.includes("vision") || desc.includes("视觉") || desc.includes("多模态")) return "多模态视觉";
    if (name.includes("pro") || id.includes("pro") || desc.includes("深度推理")) return "深度推理";
    if (name.includes("lite") || id.includes("lite") || desc.includes("超轻量")) return "超轻量";
    if (name.includes("flash") || id.includes("flash") || desc.includes("极速")) return "极速响应";
    if (name.includes("grok 3 mini") || id.includes("grok-3-mini")) return "极速体验";
    if (name.includes("grok 2") || id.includes("grok-2")) return "全能主力";
    return "推荐";
  }

  let toastTimer = null;
  function showToast(msg) {
    const t = el("chatToast");
    if (!t) return;
    t.textContent = msg;
    t.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      t.classList.remove("show");
    }, 2200);
  }

  function currentModelID() {
    if (currentSession && currentSession.model_id) return canonModelID(currentSession.model_id);
    if (pendingModel && pendingModel.id) return canonModelID(pendingModel.id);
    const list = allModels();
    if (list.length > 0) return list[0].id;
    return "supergrok";
  }

  // ── 图片附件处理 (Image Attachments) ──
  let attachedImages = [];

  function compressImage(file, maxDim, quality) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        const img = new Image();
        img.onload = () => {
          let w = img.width;
          let h = img.height;
          if (w > maxDim || h > maxDim) {
            if (w > h) {
              h = Math.round((h * maxDim) / w);
              w = maxDim;
            } else {
              w = Math.round((w * maxDim) / h);
              h = maxDim;
            }
          }
          const canvas = document.createElement("canvas");
          canvas.width = w;
          canvas.height = h;
          const ctx = canvas.getContext("2d");
          ctx.drawImage(img, 0, 0, w, h);
          resolve(canvas.toDataURL("image/jpeg", quality || 0.85));
        };
        img.onerror = reject;
        img.src = e.target.result;
      };
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
  }

  async function addImageFile(file) {
    if (!file || !file.type.startsWith("image/")) return;
    if (attachedImages.length >= 4) {
      showToast("最多同时添加 4 张图片");
      return;
    }
    try {
      const base64 = await compressImage(file, 1600, 0.85);
      attachedImages.push({ id: Date.now() + Math.random(), base64, name: file.name });
      renderImagePreviews();
      showToast("已添加图片: " + (file.name || "图片"));
    } catch (e) {
      showToast("图片读取失败");
    }
  }

  function renderImagePreviews() {
    const bar = el("imagePreviewBar");
    const attachBtn = el("attachImageBtn");
    if (!bar) return;
    if (attachedImages.length === 0) {
      bar.style.display = "none";
      bar.innerHTML = "";
      if (attachBtn) attachBtn.classList.remove("has-images");
      return;
    }
    bar.style.display = "flex";
    bar.innerHTML = "";
    if (attachBtn) attachBtn.classList.add("has-images");
    attachedImages.forEach((img, idx) => {
      const chip = document.createElement("div");
      chip.className = "image-preview-chip";
      chip.innerHTML =
        `<img src="${img.base64}" alt="preview" />` +
        `<button type="button" class="image-preview-del" title="删除图片">✕</button>`;
      chip.querySelector(".image-preview-del").onclick = (e) => {
        e.stopPropagation();
        attachedImages.splice(idx, 1);
        renderImagePreviews();
      };
      bar.appendChild(chip);
    });
  }

  function fillAssistantSelect() {
    assistantSelect.innerHTML = "";
    assistants.forEach((a) => {
      const opt = document.createElement("option");
      opt.value = a.id;
      opt.textContent = a.name;
      assistantSelect.appendChild(opt);
    });
  }

  function currentAssistant() {
    if (currentSession) return assistantById(currentSession.assistant_id);
    return assistantById(parseInt(assistantSelect.value, 10));
  }

  function updateChip() {
    const exp = el("exportBtn");
    if (exp) exp.style.display = currentSession ? "" : "none";

    // 助手 Chip（展示当前助手人格）
    const a = currentAssistant();
    if (a && !a.is_default) {
      assistantChip.style.display = "inline-flex";
      assistantChip.innerHTML =
        `<span class="chip-emoji"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg></span>` +
        `<span class="chip-name">${DW.escapeHTML(a.name)}</span>`;
    } else {
      assistantChip.style.display = "none";
    }

    // Cherry Studio 模型选择器按钮状态
    const mid = currentModelID();
    const meta = modelMeta(mid);
    const mname = (currentSession && currentSession.model_name) || (meta && (meta.name || meta.display_name)) || mid;
    const vendor = (meta && meta.vendor) || "grok";
    const tag = modelTag(meta || { name: mname, id: mid });

    const iconEl = el("mpVendorIcon");
    const nameEl = el("mpModelName");
    const tagEl = el("mpTag");

    if (iconEl) {
      const vImg = vendorImg(vendor);
      iconEl.innerHTML = `<img src="${vImg}" class="mp-vendor-img" alt="${vendor}" />`;
      iconEl.className = "mp-vendor-icon " + vendorClass(vendor);
    }
    if (nameEl) nameEl.textContent = mname;
    if (tagEl) tagEl.textContent = tag;

    if (quickPrompts) quickPrompts.classList.toggle("is-hidden", !!currentSession);
  }

  // ── Cherry Studio 浮动模型选择面板逻辑 ──
  let isDropdownOpen = false;
  function positionModelSheet() {
    const panel = el("modelDropdown");
    const btn = el("modelPickerBtn");
    if (!panel || !btn || window.innerWidth <= 680) {
      if (panel) { panel.style.top = ""; panel.style.left = ""; }
      return;
    }
    const r = btn.getBoundingClientRect();
    const width = Math.min(400, window.innerWidth - 24);
    let left = r.left;
    if (left + width > window.innerWidth - 12) left = window.innerWidth - width - 12;
    if (left < 12) left = 12;
    panel.style.top = Math.round(r.bottom + 8) + "px";
    panel.style.left = Math.round(left) + "px";
  }

  function openModelDropdown() {
    const sheet = el("modelSheet");
    const btn = el("modelPickerBtn");
    if (!sheet || !btn) return;
    isDropdownOpen = true;
    btn.classList.add("open");
    btn.setAttribute("aria-expanded", "true");
    sheet.hidden = false;
    sheet.classList.add("open");
    positionModelSheet();
    renderModelDropdownList("");
    const search = el("modelSearchInput");
    if (search) search.value = "";
  }

  function closeModelDropdown() {
    const sheet = el("modelSheet");
    const btn = el("modelPickerBtn");
    isDropdownOpen = false;
    if (btn) {
      btn.classList.remove("open");
      btn.setAttribute("aria-expanded", "false");
    }
    if (sheet) {
      sheet.classList.remove("open");
      sheet.hidden = true;
    }
  }

  function toggleModelDropdown() {
    if (isDropdownOpen) closeModelDropdown();
    else openModelDropdown();
  }

  function renderModelDropdownList(filter) {
    const listEl = el("modelDropdownList");
    if (!listEl) return;
    listEl.innerHTML = "";
    const curID = currentModelID();
    const q = (filter || "").trim().toLowerCase();

    let totalShown = 0;
    vendors.forEach((v) => {
      const vid = (v.id || "").toLowerCase();
      const vname = (v.display_name || "").toLowerCase();
      const modelsInVendor = (v.models || []).filter((m) => {
        if (!q) return true;
        const n = (m.name || m.display_name || m.id || "").toLowerCase();
        const d = (m.desc || m.description || "").toLowerCase();
        const vbrand = (m.vendor || "").toLowerCase();
        return n.includes(q) || d.includes(q) || vid.includes(q) || vname.includes(q) || vbrand.includes(q);
      });

      if (modelsInVendor.length === 0) return;

      const groupHeader = document.createElement("div");
      groupHeader.className = "md-vendor-group";
      groupHeader.textContent = v.display_name;
      listEl.appendChild(groupHeader);

      modelsInVendor.forEach((m) => {
        totalShown++;
        const isAct = m.id === curID;
        const item = document.createElement("div");
        item.className = "md-item" + (isAct ? " active" : "");
        const vImg = vendorImg(m.vendor || v.id);
        const vCls = vendorClass(m.vendor || v.id);
        const tag = modelTag(m);
        const desc = m.desc || m.description || "";

        item.innerHTML =
          `<span class="md-item-icon ${vCls}"><img src="${vImg}" class="md-vendor-img" alt="${v.id}" /></span>` +
          `<div class="md-item-info">` +
          `  <div class="md-item-head">` +
          `    <span class="md-item-name">${DW.escapeHTML(m.name || m.id)}</span>` +
          `    <span class="md-item-badge">${DW.escapeHTML(tag)}</span>` +
          `  </div>` +
          `  ${desc ? `<div class="md-item-desc">${DW.escapeHTML(desc)}</div>` : ""}` +
          `</div>` +
          `<span class="md-item-check">✓</span>`;

        item.dataset.mid = m.id;
        item.setAttribute("role", "menuitem");
        listEl.appendChild(item);
      });
    });

    if (totalShown === 0) {
      const empty = document.createElement("div");
      empty.style.padding = "24px 16px";
      empty.style.textAlign = "center";
      empty.style.color = "var(--text-faint)";
      empty.style.fontSize = "13px";
      empty.textContent = allModels().length === 0 ? "暂无可用模型（管理员需先配置并探测通过 API）" : "未找到匹配模型";
      listEl.appendChild(empty);
    }
  }

  function switchModel(modelID) {
    if (!modelID) return;
    const id = canonModelID(modelID);
    const m = modelMeta(id) || { id, name: id, vendor: "grok" };
    const mName = m.name || m.display_name || id;
    const targetID = m.id || id;

    // 立即关闭下拉菜单，给用户零延迟反馈
    closeModelDropdown();

    if (currentSession) {
      currentSession.model_id = targetID;
      currentSession.model_name = mName;
      updateChip();
      renderSessions();
      showToast("已切换至 " + mName);
      DW.api("/api/sessions/" + currentSession.id + "/model", {
        method: "POST",
        body: JSON.stringify({ model_id: targetID })
      }).catch((e) => {
        showToast("模型同步失败: " + (e.message || "未知错误"));
      });
    } else {
      pendingModel = { id: targetID, name: mName, vendor: m.vendor };
      updateChip();
      showEmpty();
      showToast("已选择 " + mName);
    }
  }

  function sessionBucket(s) {
    const raw = (s.updated_at || "").replace(" ", "T");
    const t = new Date(raw);
    if (Number.isNaN(t.getTime())) return DW.t("chat.sess.earlier");
    const now = new Date();
    const start = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const diff = (start(now) - start(t)) / 86400000;
    if (diff < 1) return DW.t("chat.sess.today");
    if (diff < 2) return DW.t("chat.sess.yesterday");
    return DW.t("chat.sess.earlier");
  }

  function renderSessions() {
    sessionListEl.innerHTML = "";
    let last = "";
    sessions.filter((s) => (s.msg_count || 0) > 0).forEach((s) => {
      const bucket = sessionBucket(s);
      if (bucket !== last) {
        last = bucket;
        const h = document.createElement("div");
        h.className = "session-group";
        h.textContent = bucket;
        sessionListEl.appendChild(h);
      }
      const item = document.createElement("div");
      item.className = "session-item" + (currentSession && s.id === currentSession.id ? " active" : "");
      item.innerHTML =
        `<div style="flex:1; min-width:0;">` +
        `<span class="s-title">${DW.escapeHTML(s.title)}</span>` +
        `<span class="s-sub">${DW.escapeHTML(s.model_name || s.model_id || "")}</span></div>` +
        `<button class="s-act" data-a="rename" title="${DW.t("chat.rename")}">✎</button>` +
        `<button class="s-del" title="delete">🗑</button>`;
      item.querySelector(".s-title").onclick = () => selectSession(s);
      item.querySelector(".s-sub").onclick = () => selectSession(s);
      item.querySelector('[data-a="rename"]').onclick = (e) => { e.stopPropagation(); renameSession(s); };
      item.querySelector(".s-del").onclick = (e) => { e.stopPropagation(); deleteSession(s); };
      sessionListEl.appendChild(item);
    });
  }

  async function loadSessions() {
    sessions = (await DW.api("/api/sessions")) || [];
    renderSessions();
  }

  async function selectSession(s) {
    currentSession = s;
    pendingModel = null;
    if (s && s.assistant_id) assistantSelect.value = s.assistant_id;
    renderSessions();
    updateChip();
    closeSidebar();
    await loadMessages();
  }

  async function deleteSession(s) {
    try {
      await DW.api("/api/sessions/" + s.id, { method: "DELETE" });
      if (currentSession && currentSession.id === s.id) { currentSession = null; showEmpty(); }
      showToast("对话已删除");
      await loadSessions();
    } catch (e) {
      showToast(e.message || "删除失败");
    }
  }

  async function renameSession(s) {
    const title = prompt(DW.t("chat.rename.prompt"), s.title);
    if (title === null) return;
    const t = title.trim();
    if (!t) return;
    try {
      await DW.api("/api/sessions/" + s.id + "/title", { method: "POST", body: JSON.stringify({ title: t }) });
      s.title = t;
      if (currentSession && currentSession.id === s.id) currentSession.title = t;
      renderSessions();
      showToast("已重命名为: " + t);
    } catch (e) { showToast(e.message || DW.t("common.error")); }
  }

  async function exportChat() {
    if (!currentSession) return;
    const msgs = (await DW.api("/api/sessions/" + currentSession.id + "/messages")) || [];
    const a = assistantById(currentSession.assistant_id);
    const lines = [
      `# ${currentSession.title}`, "",
      `- ${DW.t("chat.assistant")}: ${a ? a.name : "-"}`,
      `- Model: ${currentSession.model_name || currentSession.model_id}`,
      `- Exported: ${new Date().toISOString().slice(0, 16).replace("T", " ")}`,
      "", "---", "",
    ];
    msgs.forEach((m) => {
      const role = m.role === "user" ? "User" : "Assistant";
      lines.push("");
      lines.push(m.content);
      lines.push("");
    });
    const blob = new Blob([lines.join("\n")], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = (currentSession.title || "chat").replace(/[\/\\:*?"<>|]/g, "_") + ".md";
    link.click();
    URL.revokeObjectURL(url);
    showToast("对话已导出");
  }

  function greeting() {
    const h = new Date().getHours();
    if (h < 5) return DW.t("chat.hello.night");
    if (h < 12) return DW.t("chat.hello.morning");
    if (h < 18) return DW.t("chat.hello.afternoon");
    return DW.t("chat.hello.evening");
  }

  function showEmpty() {
    const curID = currentModelID();
    const list = allModels();
    const tiles = list.map((m) => {
      const active = curID === m.id ? " active" : "";
      const vImg = vendorImg(m.vendor);
      const vCls = vendorClass(m.vendor);
      const tag = modelTag(m);
      const desc = (m.desc || m.description || "").split(" · ").slice(0, 2).join(" · ");
      return `<button type="button" class="model-tile${active}" data-mid="${DW.escapeHTML(m.id)}">` +
        `<div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:6px;">` +
        `  <span class="mp-vendor-icon ${vCls}" style="width:22px;height:22px;"><img src="${vImg}" class="mp-tile-img" alt="" /></span>` +
        `  <span class="mp-tag" style="font-size:9.5px;padding:1px 6px;">${DW.escapeHTML(tag)}</span>` +
        `</div>` +
        `<span class="t-name">${DW.escapeHTML(m.name || m.id)}</span>` +
        `<span class="t-desc">${DW.escapeHTML(desc || m.vendor || "")}</span></button>`;
    }).join("");
    messagesEl.innerHTML =
      `<div class="chat-empty">` +
      `<div class="hero-cosmic-loop">` +
      `  <div class="cosmic-aura"></div>` +
      `  <div class="cosmic-orbit orbit-outer"></div>` +
      `  <div class="cosmic-orbit orbit-inner"></div>` +
      `  <div class="cosmic-core">` +
      `    <span class="cosmic-sparkle"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg></span>` +
      `  </div>` +
      `</div>` +
      `<h2 class="hello">${DW.escapeHTML(greeting())}</h2>` +
      `<p class="sub">${DW.t("chat.empty.desc")}</p>` +
      `<div class="model-tiles">${tiles}</div>` +
      `<div class="suggest-grid">` +
      `<button type="button" class="suggest-card" data-prompt="请帮我重构并优化以下代码，提升可读性和性能：\n\n">${DW.t("chat.sug.code")}</button>` +
      `<button type="button" class="suggest-card" data-prompt="请将以下内容进行长文润色与结构梳理，用清晰优雅的中文表达：\n\n">${DW.t("chat.sug.write")}</button>` +
      `<button type="button" class="suggest-card" data-prompt="请将以下内容翻译为准确地道的英文：\n\n">${DW.t("chat.sug.translate")}</button>` +
      `<button type="button" class="suggest-card" data-prompt="请用通俗易懂且逻辑严密的语言，详细解释以下概念：\n\n">${DW.t("chat.sug.explain")}</button>` +
      `</div></div>`;

    messagesEl.querySelectorAll(".model-tile").forEach((btn) => {
      btn.onclick = () => {
        switchModel(btn.dataset.mid);
        input.focus();
      };
    });
    messagesEl.querySelectorAll(".suggest-card").forEach((btn) => {
      btn.onclick = () => {
        input.value = btn.dataset.prompt.replace(/\\n/g, "\n");
        autoGrow();
        input.focus();
      };
    });
    updateChip();
  }

  function renderMarkdown(text) {
    if (!text) return "";
    let html = DW.escapeHTML(text || "");

    // Markdown Images: ![alt](url)
    html = html.replace(/!\[([^\]]*)\]\((https?:\/\/[^\s\)]+|data:image\/[^\s\)]+)\)/g, (m, alt, src) => {
      const cleanSrc = src.replace(/&amp;/g, "&");
      return `<img src="${cleanSrc}" alt="${alt}" class="chat-inline-img" onclick="window.open(this.src)" />`;
    });

    // Code blocks with syntax copy button
    html = html.replace(/```([\w-]*)\n([\s\S]*?)```/g, (_, lang, code) => {
      const rawCode = code.replace(/\n$/, "");
      return `<div class="code-block-wrap"><div class="code-block-bar"><span class="code-lang">${lang || "code"}</span><button class="code-copy-btn" type="button" onclick="navigator.clipboard.writeText(this.closest('.code-block-wrap').querySelector('code').innerText);this.textContent='已复制';setTimeout(()=>this.textContent='复制',1500);">复制</button></div><pre class="md-pre"><code>${rawCode}</code></pre></div>`;
    });

    html = html.replace(/`([^`]+)`/g, "<code class=\"md-code\">$1</code>");
    html = html.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    html = html.replace(/^### (.+)$/gm, "<h4>$1</h4>");
    html = html.replace(/^## (.+)$/gm, "<h3>$1</h3>");
    html = html.replace(/^# (.+)$/gm, "<h3>$1</h3>");
    html = html.replace(/^\s*[-*] (.+)$/gm, "<li>$1</li>");
    html = html.replace(/(?:<li>.*<\/li>\n?)+/g, (m) => `<ul class="md-ul">${m}</ul>`);
    html = html.replace(/^\s*&gt; (.+)$/gm, "<blockquote>$1</blockquote>");
    html = html.replace(/(?:<blockquote>.*<\/blockquote>\n?)+/g, (m) => `<div class="chat-blockquote">${m}</div>`);
    html = html.replace(/\n/g, "<br>");
    html = html.replace(/<\/div><br>/g, "</div>").replace(/<br><div/g, "<div");
    html = html.replace(/<\/pre><br>/g, "</pre>").replace(/<br><pre/g, "<pre");
    html = html.replace(/<\/ul><br>/g, "</ul>").replace(/<br><ul/g, "<ul");
    html = html.replace(/<br><\/li>/g, "</li>");
    return html;
  }

  function renderThinkingMarkdown(text) {
    if (!text) return "";
    const tIdx = text.indexOf("<think>");
    if (tIdx === -1) return renderMarkdown(text);

    const prefix = text.slice(0, tIdx);
    const afterThink = text.slice(tIdx + 7);
    const endIdx = afterThink.indexOf("</think>");

    if (endIdx !== -1) {
      const thinkText = afterThink.slice(0, endIdx).trim();
      const answerText = afterThink.slice(endIdx + 8).trim();
      let res = "";
      if (prefix) res += renderMarkdown(prefix);
      if (thinkText) {
        res +=
          `<div class="think-box">` +
          `  <div class="think-head" onclick="this.parentElement.classList.toggle('open')">` +
          `    <span class="think-icon">🧠</span>` +
          `    <span class="think-title">深度思考过程 (点击展开/折叠)</span>` +
          `    <span class="think-toggle">▾</span>` +
          `  </div>` +
          `  <div class="think-body">${renderMarkdown(thinkText)}</div>` +
          `</div>`;
      }
      if (answerText) res += renderMarkdown(answerText);
      return res;
    }

    // 仍在流式思考中
    let res = "";
    if (prefix) res += renderMarkdown(prefix);
    res +=
      `<div class="think-box open is-thinking">` +
      `  <div class="think-head" onclick="this.parentElement.classList.toggle('open')">` +
      `    <span class="think-icon">🧠</span>` +
      `    <span class="think-title">正在深度思考中...</span>` +
      `    <span class="think-pulse"><i></i><i></i><i></i></span>` +
      `    <span class="think-toggle">▾</span>` +
      `  </div>` +
      `  <div class="think-body">${renderMarkdown(afterThink.trim())}</div>` +
      `</div>`;
    return res;
  }

  function setContent(contentEl, text, asMarkdown) {
    if (asMarkdown) contentEl.innerHTML = renderThinkingMarkdown(text);
    else contentEl.textContent = text;
  }

  function addMessage(role, content, opts) {
    const empty = messagesEl.querySelector(".chat-empty");
    if (empty) empty.remove();
    const wrap = document.createElement("div");
    wrap.className = "msg-wrap" + (opts && opts.silent ? "" : " msg-enter");
    const mid = (currentSession && currentSession.model_id) || currentModelID();
    const avatar = role === "user"
      ? `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path><circle cx="12" cy="7" r="4"></circle></svg>`
      : `<img src="${vendorImg(mid)}" style="width:17px;height:17px;object-fit:contain;border-radius:3px;" alt="" />`;
    let actionsHTML = `<button class="msg-copy" type="button">${DW.t("chat.copy")}</button>`;
    if (role === "user") {
      lastUserMessageText = content;
      actionsHTML += ` <button class="msg-edit" type="button">${DW.t("chat.edit")}</button>`;
    } else {
      actionsHTML += ` <button class="msg-tts" type="button">${DW.t("chat.speak")}</button>`;
      actionsHTML += ` <button class="msg-regen" type="button">${DW.t("chat.regen")}</button>`;
    }
    wrap.innerHTML =
      `<div class="msg ${role}"><div class="avatar">${avatar}</div>` +
      `<div class="body"><div class="content"></div>` +
      `<div class="msg-actions">${actionsHTML}</div>` +
      `<div class="meta"></div></div></div>`;
    const contentEl = wrap.querySelector(".content");
    if (role === "assistant" && !content) {
      contentEl.classList.add("is-typing");
      contentEl.innerHTML = '<span class="typing-dots" aria-hidden="true"><i></i><i></i><i></i></span>';
    } else if (role === "assistant") {
      setContent(contentEl, content, true);
    } else {
      contentEl.innerHTML = renderMarkdown(content);
    }

    wrap.querySelector(".msg-copy").onclick = async () => {
      try {
        await navigator.clipboard.writeText(contentEl.textContent);
        wrap.querySelector(".msg-copy").textContent = DW.t("chat.copied");
        setTimeout(() => { wrap.querySelector(".msg-copy").textContent = DW.t("chat.copy"); }, 1500);
      } catch (e) {}
    };
    const editBtn = wrap.querySelector(".msg-edit");
    if (editBtn) {
      editBtn.onclick = () => {
        // 过滤掉 markdown 图片语法，填回纯文本
        input.value = content.replace(/!\[image\]\(data:image\/[^\)]+\)/g, "").trim();
        autoGrow();
        input.focus();
      };
    }
    const ttsBtn = wrap.querySelector(".msg-tts");
    if (ttsBtn) {
      ttsBtn.onclick = () => {
        if (!("speechSynthesis" in window)) return;
        if (window.speechSynthesis.speaking) {
          window.speechSynthesis.cancel();
          ttsBtn.textContent = DW.t("chat.speak");
          return;
        }
        const u = new SpeechSynthesisUtterance(contentEl.textContent);
        u.onend = () => { ttsBtn.textContent = DW.t("chat.speak"); };
        ttsBtn.textContent = DW.t("chat.stop");
        window.speechSynthesis.speak(u);
      };
    }
    const regenBtn = wrap.querySelector(".msg-regen");
    if (regenBtn) {
      regenBtn.onclick = () => {
        if (streaming || !lastUserMessageText) return;
        input.value = lastUserMessageText;
        send();
      };
    }
    messagesEl.appendChild(wrap);
    scrollToBottom();
    return contentEl;
  }

  function scrollToBottom() { messagesEl.scrollTop = messagesEl.scrollHeight; }

  async function loadMessages() {
    if (!currentSession) { showEmpty(); return; }
    messagesEl.innerHTML = "";
    const msgs = (await DW.api("/api/sessions/" + currentSession.id + "/messages")) || [];
    if (msgs.length === 0) { showEmpty(); return; }
    msgs.forEach((m) => {
      const c = addMessage(m.role, m.content, { silent: true });
      if (m.role === "assistant" && m.cost_usd > 0) {
        c.parentElement.querySelector(".meta").textContent =
          `${DW.t("chat.cost")}: $${m.cost_usd.toFixed(6)}${m.estimated ? " (" + DW.t("chat.estimated") + ")" : ""}`;
      }
    });
    updateChip();
  }

  async function ensureSession() {
    if (currentSession) return currentSession;
    const assistantID = parseInt(assistantSelect.value, 10) || 0;
    const body = { assistant_id: assistantID };
    if (pendingModel && pendingModel.id) body.model_id = pendingModel.id;
    const s = await DW.api("/api/sessions", { method: "POST", body: JSON.stringify(body) });
    currentSession = s;
    updateChip();
    return s;
  }

  function setStreamingUI(on) {
    streaming = on;
    sendBtn.classList.toggle("sending", on);
    sendBtn.textContent = on ? "■" : "➤";
    sendBtn.setAttribute("aria-label", on ? "stop" : "send");
    const composer = document.querySelector(".composer");
    if (composer) composer.classList.toggle("is-streaming", on);
    if (!on) sendBtn.disabled = false;
  }

  let isWebSearch = localStorage.getItem("dw_web_search") === "1";
  let isThinking = localStorage.getItem("dw_thinking") === "1";

  function syncToolToggles() {
    const wsBtn = el("toggleWebSearch");
    if (wsBtn) wsBtn.classList.toggle("active", isWebSearch);
    const thBtn = el("toggleThinking");
    if (thBtn) thBtn.classList.toggle("active", isThinking);
  }

  let sendLock = false;
  async function send() {
    if (streaming) {
      if (streamAbort) streamAbort.abort();
      return;
    }
    if (sendLock) return;
    const text = input.value.trim();
    if (!text && attachedImages.length === 0) return;
    sendLock = true;
    setStreamingUI(true);
    input.value = "";
    autoGrow();

    let sess;
    try { sess = await ensureSession(); }
    catch (e) {
      addMessage("assistant", "⚠ " + (e.message || DW.t("common.error")));
      sendLock = false;
      setStreamingUI(false);
      return;
    }

    const currentImages = [...attachedImages];
    attachedImages = [];
    renderImagePreviews();

    let userDisplay = text;
    if (currentImages.length > 0) {
      currentImages.forEach(img => {
        userDisplay += (userDisplay ? "\n\n" : "") + `![image](${img.base64})`;
      });
    }

    addMessage("user", userDisplay || "请分析这张图片");
    const contentEl = addMessage("assistant", "");
    contentEl.classList.add("cursor-blink");
    streamAbort = new AbortController();

    try {
      const streamHeaders = { "Content-Type": "application/json" };
      try {
        const token = localStorage.getItem("dw_token") || "";
        if (token) streamHeaders.Authorization = "Bearer " + token;
      } catch (e) {}
      const res = await fetch("/api/chat/stream", {
        method: "POST",
        headers: streamHeaders,
        credentials: "same-origin",
        signal: streamAbort.signal,
        body: JSON.stringify({
          session_id: sess.id,
          content: text,
          images: currentImages.map(img => img.base64),
          web_search: isWebSearch,
          thinking: isThinking,
        }),
      });
      if (res.status === 401) { location.replace("/login?next=" + encodeURIComponent("/chat")); return; }
      const ctype = res.headers.get("content-type") || "";
      if (!res.ok && ctype.includes("application/json")) {
        const j = await res.json().catch(() => ({}));
        contentEl.classList.remove("cursor-blink", "is-typing");
        contentEl.textContent = "⚠ " + (j.error || DW.t("common.error"));
        if (j.paywall) showPaywall({ needsPro: j.needs_pro, message: j.error, nextReset: j.next_reset });
        return;
      }
      if (!res.body) throw new Error("no stream");

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "", acc = "", hadError = false;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop();
        for (const line of lines) {
          const l = line.trim();
          if (!l.startsWith("data:")) continue;
          const data = l.slice(5).trim();
          if (data === "[DONE]") continue;
          try {
            const obj = JSON.parse(data);
            if (obj.error) { hadError = true; acc += (acc ? "\n\n" : "") + "⚠ " + obj.error; }
            else if (obj.choices && obj.choices[0] && obj.choices[0].delta) {
              const delta = obj.choices[0].delta;
              const rDelta = delta.reasoning_content || delta.reasoning;
              if (rDelta && !acc.includes("<think>")) {
                acc = "<think>\n" + rDelta;
              } else if (rDelta) {
                acc += rDelta;
              } else if (delta.content) {
                if (acc.startsWith("<think>") && !acc.includes("</think>")) {
                  acc += "\n</think>\n\n" + delta.content;
                } else {
                  acc += delta.content;
                }
              }
            }
            if (contentEl.classList.contains("is-typing")) {
              contentEl.classList.remove("is-typing");
              contentEl.textContent = "";
            }
            if (acc.includes("<think>")) {
              contentEl.innerHTML = renderThinkingMarkdown(acc);
            } else {
              contentEl.textContent = acc;
            }
            scrollToBottom();
          } catch (e) {}
        }
      }
      contentEl.classList.remove("cursor-blink", "is-typing");
      if (!acc) { contentEl.textContent = "⚠ " + DW.t("common.error"); hadError = true; }
      else setContent(contentEl, acc, true);
      setTimeout(loadSessions, 1200);
      if (!hadError) { setTimeout(loadMessages, 1400); setTimeout(maybeSoftNudge, 1600); }
    } catch (e) {
      contentEl.classList.remove("cursor-blink", "is-typing");
      if (e.name === "AbortError") {
        if (!contentEl.textContent || contentEl.classList.contains("is-typing")) {
          contentEl.classList.remove("is-typing");
          contentEl.textContent = DW.t("chat.stopped");
        }
      } else {
        contentEl.textContent = "⚠ " + DW.t("common.error");
      }
    } finally {
      streamAbort = null;
      sendLock = false;
      setStreamingUI(false);
    }
  }

  function autoGrow() {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 180) + "px";
  }
  input.addEventListener("input", autoGrow);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }
  });
  sendBtn.onclick = send;
  assistantSelect.onchange = () => { updateChip(); };

  // ── 联网搜索 & 深度思考按钮事件 ──
  const wsBtn = el("toggleWebSearch");
  if (wsBtn) {
    wsBtn.onclick = () => {
      isWebSearch = !isWebSearch;
      localStorage.setItem("dw_web_search", isWebSearch ? "1" : "0");
      syncToolToggles();
      showToast(isWebSearch ? "联网搜索已开启" : "联网搜索已关闭");
    };
  }
  const thBtn = el("toggleThinking");
  if (thBtn) {
    thBtn.onclick = () => {
      isThinking = !isThinking;
      localStorage.setItem("dw_thinking", isThinking ? "1" : "0");
      syncToolToggles();
      showToast(isThinking ? "深度思考已开启" : "深度思考已关闭");
    };
  }

  // ── 图片附件事件监听 ──
  const imgInput = el("imageFileInput");
  const attachBtn = el("attachImageBtn");
  if (attachBtn && imgInput) {
    attachBtn.onclick = () => imgInput.click();
    imgInput.onchange = (e) => {
      const files = Array.from(e.target.files || []);
      files.forEach((f) => addImageFile(f));
      imgInput.value = "";
    };
  }

  window.addEventListener("paste", (e) => {
    if (!e.clipboardData || !e.clipboardData.items) return;
    for (const item of e.clipboardData.items) {
      if (item.kind === "file" && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (file) {
          e.preventDefault();
          addImageFile(file);
        }
      }
    }
  });

  const composerDock = document.querySelector(".composer-dock");
  if (composerDock) {
    composerDock.addEventListener("dragover", (e) => {
      e.preventDefault();
      composerDock.classList.add("drag-over");
    });
    composerDock.addEventListener("dragleave", (e) => {
      e.preventDefault();
      composerDock.classList.remove("drag-over");
    });
    composerDock.addEventListener("drop", (e) => {
      e.preventDefault();
      composerDock.classList.remove("drag-over");
      const files = Array.from((e.dataTransfer && e.dataTransfer.files) || []);
      const imgFiles = files.filter((f) => f.type && f.type.startsWith("image/"));
      if (imgFiles.length > 0) {
        imgFiles.forEach((f) => addImageFile(f));
      }
    });
  }

  // ── Cherry Studio 模型选择器事件 ──
  const mpBtn = el("modelPickerBtn");
  if (mpBtn) {
    mpBtn.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      toggleModelDropdown();
    });
  }
  const mpList = el("modelDropdownList");
  if (mpList) {
    mpList.addEventListener("click", (e) => {
      const item = e.target.closest("[data-mid]");
      if (!item) return;
      e.preventDefault();
      switchModel(item.getAttribute("data-mid"));
    });
  }
  const mpBackdrop = el("modelSheetBackdrop");
  if (mpBackdrop) mpBackdrop.addEventListener("click", closeModelDropdown);
  const mpSearch = el("modelSearchInput");
  if (mpSearch) {
    mpSearch.oninput = (e) => renderModelDropdownList(e.target.value);
  }
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && isDropdownOpen) closeModelDropdown();
  });
  window.addEventListener("resize", () => {
    if (isDropdownOpen) positionModelSheet();
  });

  const fxBtn = el("fxBtn");
  if (fxBtn) {
    fxBtn.onclick = () => {
      const cur = document.documentElement.getAttribute("data-fx") || "rich";
      const next = cur === "rich" ? "off" : "rich";
      document.documentElement.setAttribute("data-fx", next);
      localStorage.setItem("dw_chat_fx", next);
      fxBtn.textContent = next === "rich" ? "✦" : "·";
    };
  }

  const handleNewChat = () => {
    const lastMid = currentModelID();
    currentSession = null;
    if (lastMid) {
      const m = modelMeta(lastMid);
      pendingModel = { id: lastMid, name: (m && (m.name || m.display_name)) || lastMid, vendor: (m && m.vendor) || "grok" };
    }
    showEmpty();
    renderSessions();
    closeSidebar();
    updateChip();
    input.focus();
  };
  const nb = el("newChatBtn");
  if (nb) nb.onclick = handleNewChat;
  const tnb = el("topbarNewChatBtn");
  if (tnb) tnb.onclick = handleNewChat;
  el("exportBtn").onclick = exportChat;
  el("pwLater").onclick = hidePaywall;
  el("paywall").addEventListener("click", (e) => { if (e.target === el("paywall")) hidePaywall(); });

  document.querySelectorAll(".quick-prompts .preset-btn").forEach((btn) => {
    btn.onclick = () => {
      input.value = (btn.dataset.prompt || "").replace(/\\n/g, "\n");
      autoGrow();
      input.focus();
    };
  });

  async function init() {
    const me = await DW.whoami();
    if (me && me.is_admin) el("adminLink").style.display = "";
    if (fxBtn && (localStorage.getItem("dw_chat_fx") || "rich") === "off") fxBtn.textContent = "·";
    try { applyVendors(await DW.api("/api/models")); } catch (e) { applyVendors(null); }
    assistants = (await DW.api("/api/assistants")) || [];
    fillAssistantSelect();
    syncToolToggles();
    await loadSessions();
    updateChip();
    showEmpty();
    try {
      const bill = await DW.api("/api/billing/info");
      refreshQuota(bill);
    } catch (e) {}
  }

  document.addEventListener("dw:ready", init);
  document.addEventListener("dw:langchange", () => { if (!currentSession) showEmpty(); else updateChip(); });
})();
