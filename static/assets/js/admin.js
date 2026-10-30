// ── Deepweay 管理后台（多服务商管理 + 模型自动探测 + 动态上架 + 计费）───
(function () {
  const el = (id) => document.getElementById(id);
  let me = null;
  let allProviders = [];
  let allModels = [];
  let allUsers = [];
  let currentDetectProvider = null;
  let detectedList = [];

  function fmtUSD(n) { return n >= 0.01 ? "$" + n.toFixed(2) : "$" + n.toFixed(6); }
  function can(perm) {
    return !!(me && Array.isArray(me.permissions) && me.permissions.includes(perm));
  }
  function roleRank(role) {
    return { user: 0, staff: 1, admin: 2, owner: 3 }[role] || 0;
  }
  function userRole(u) {
    return u.role || (u.is_admin ? "admin" : "user");
  }
  function canActOn(u) {
    const ar = me && me.role, tr = userRole(u);
    if (!ar) return false;
    if (ar === "owner" && tr !== "owner") return true;
    return roleRank(ar) > roleRank(tr);
  }
  function assignableRoles() {
    if (!me) return ["user"];
    if (me.role === "owner") return ["user", "staff", "admin"];
    if (me.role === "admin") return ["user", "staff"];
    return ["user"];
  }
  function applyPerms() {
    document.querySelectorAll("[data-need]").forEach((n) => {
      n.style.display = can(n.getAttribute("data-need")) ? "" : "none";
    });
    const tag = el("myRoleTag");
    if (tag && me && me.role) {
      tag.style.display = "";
      tag.className = "tag role-" + me.role;
      tag.textContent = DW.t("admin.role." + me.role);
    }
    const sel = el("newRole");
    if (sel && can("users.role")) {
      sel.innerHTML = assignableRoles()
        .map((r) => `<option value="${r}">${DW.t("admin.role." + r)}</option>`)
        .join("");
    }
  }

  async function loadStats() {
    try {
      const s = await DW.api("/api/admin/stats");
      el("stUsers").textContent = s.total_users || 0;
      el("stMessages").textContent = s.total_messages || 0;
      el("stSpend").textContent = fmtUSD(s.total_spend || 0);
      el("stActive").textContent = s.active_7d || 0;
    } catch (e) {}
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 1. 服务商管理 (Cherry Studio 风格多 Provider)
  // ═══════════════════════════════════════════════════════════════════════════

  async function loadProviders() {
    try {
      allProviders = (await DW.api("/api/admin/providers")) || [];
      renderProviders();
    } catch (e) {
      console.error("load providers failed", e);
    }
  }

  function renderProviders() {
    const list = el("providerList");
    list.innerHTML = "";
    if (allProviders.length === 0) {
      list.innerHTML = `<div style="grid-column: 1/-1; text-align:center; padding:32px; color:var(--text-dim);">
        尚未添加任何服务商，请点击右上角「➕ 添加服务商」添加 Sub2API 专线或各大模型 API。
      </div>`;
      return;
    }

    allProviders.forEach((p) => {
      const card = document.createElement("div");
      card.className = "p-card" + (p.is_enabled ? "" : " disabled");

      const keyShow = p.key_preview || (p.api_key ? "••••已配置" : "未设置 Key");
      card.innerHTML = `
        <div class="p-head">
          <div class="p-title">
            <span>${DW.escapeHTML(p.name)}</span>
          </div>
          <label class="switch">
            <input type="checkbox" class="p-toggle" data-id="${p.id}" ${p.is_enabled ? "checked" : ""} />
            <span class="slider"></span>
          </label>
        </div>
        <div class="p-meta">
          <div><strong style="color:var(--text-dim);">URL:</strong> ${DW.escapeHTML(p.base_url)}</div>
          <div style="margin-top:2px;"><strong style="color:var(--text-dim);">Key:</strong> ${DW.escapeHTML(keyShow)}</div>
        </div>
        <div style="display:flex; align-items:center; justify-content:space-between; margin-top:4px;">
          <span class="badge-vendor" style="font-size:12px;">已收录 <strong>${p.model_count || 0}</strong> 个模型</span>
          <span style="font-size:11px; color:var(--text-faint);">${p.created_at || ""}</span>
        </div>
        <div class="p-actions">
          <button class="btn btn-ghost btn-sm p-probe-btn" data-id="${p.id}">探测连通</button>
          <button class="btn btn-ghost btn-sm p-detect-btn" data-id="${p.id}">拉取模型</button>
          <button class="btn btn-ghost btn-sm p-edit-btn" data-id="${p.id}">编辑</button>
          <button class="btn btn-ghost btn-sm p-del-btn" data-id="${p.id}" style="color:#e0674f;">删除</button>
        </div>
      `;
      list.appendChild(card);
    });

    // 绑定卡片内事件
    list.querySelectorAll(".p-toggle").forEach((chk) => {
      chk.onchange = async () => {
        const id = chk.dataset.id;
        try {
          await DW.api(`/api/admin/providers/${id}/toggle`, {
            method: "POST",
            body: JSON.stringify({ is_enabled: chk.checked }),
          });
          await loadProviders();
          await loadModels();
        } catch (e) {
          alert(e.message || "切换失败");
          chk.checked = !chk.checked;
        }
      };
    });

    list.querySelectorAll(".p-probe-btn").forEach((b) => {
      b.onclick = async () => {
        const p = allProviders.find((x) => x.id === parseInt(b.dataset.id, 10));
        if (!p) return;
        const old = b.textContent;
        b.disabled = true;
        b.textContent = "探测中…";
        try {
          const res = await DW.api("/api/admin/providers/probe", {
            method: "POST",
            body: JSON.stringify({ provider_id: p.id }),
          });
          alert((res.ok ? "✓ " : "✗ ") + (res.message || "探测完成") + (res.latency_ms ? `（${res.latency_ms}ms）` : ""));
          await loadModels();
          await loadProviders();
        } catch (e) {
          alert(e.message || "探测失败");
        } finally {
          b.disabled = false;
          b.textContent = old;
        }
      };
    });
    list.querySelectorAll(".p-detect-btn").forEach((b) => {
      b.onclick = () => {
        const p = allProviders.find((x) => x.id === parseInt(b.dataset.id, 10));
        if (p) openDetectModal(p);
      };
    });

    list.querySelectorAll(".p-edit-btn").forEach((b) => {
      b.onclick = () => {
        const p = allProviders.find((x) => x.id === parseInt(b.dataset.id, 10));
        if (p) openProviderModal(p);
      };
    });

    list.querySelectorAll(".p-del-btn").forEach((b) => {
      b.onclick = async () => {
        const id = b.dataset.id;
        const p = allProviders.find((x) => x.id === parseInt(id, 10));
        if (!confirm(`确定要删除服务商「${p ? p.name : id}」及其下关联的模型吗？`)) return;
        try {
          await DW.api(`/api/admin/providers/${id}`, { method: "DELETE" });
          await loadProviders();
          await loadModels();
        } catch (e) {
          alert(e.message || "删除失败");
        }
      };
    });
  }

  function openProviderModal(p) {
    el("pModalErr").textContent = "";
    if (p) {
      el("pModalTitle").textContent = "編輯服務商";
      el("pEditID").value = p.id;
      el("pName").value = p.name;
      el("pBaseURL").value = p.base_url;
      el("pAPIKey").value = "";
      el("pAPIKey").placeholder = p.api_key ? "已設置 Key（留空表示不修改）" : "填入 API Key";
      el("pEnabled").checked = p.is_enabled;
    } else {
      el("pModalTitle").textContent = "添加服務商";
      el("pEditID").value = "";
      el("pName").value = "";
      el("pBaseURL").value = "";
      el("pAPIKey").value = "";
      el("pAPIKey").placeholder = "填入 API Key / 令牌";
      el("pEnabled").checked = true;
    }
    el("providerModal").classList.add("show");
    setTimeout(() => el("pName").focus(), 80);
  }

  function closeProviderModal() {
    el("providerModal").classList.remove("show");
  }

  async function saveProvider(autoDetect = false) {
    const errBox = el("pModalErr");
    const saveBtn = el("pModalSave");
    const saveOnlyBtn = el("pModalSaveOnly");
    errBox.textContent = "";
    const id = el("pEditID").value;
    const name = el("pName").value.trim();
    const base_url = el("pBaseURL").value.trim();
    const api_key = el("pAPIKey").value.trim();
    const is_enabled = el("pEnabled").checked;

    if (!name || !base_url) {
      errBox.textContent = "服務商名稱與 Base URL 不能為空";
      return;
    }

    if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = "探測中..."; }
    if (saveOnlyBtn) saveOnlyBtn.disabled = true;

    const type = base_url.toLowerCase().includes("openrouter") ? "openrouter" : "openai_compatible";

    try {
      let saved = null;
      if (id) {
        saved = await DW.api(`/api/admin/providers/${id}`, {
          method: "PUT",
          body: JSON.stringify({ name, type, base_url, api_key, is_enabled }),
        });
      } else {
        saved = await DW.api("/api/admin/providers", {
          method: "POST",
          body: JSON.stringify({ name, type, base_url, api_key, is_enabled }),
        });
      }
      const probe = saved && saved.probe;
      if (probe) {
        errBox.style.color = probe.ok ? "var(--accent)" : "";
        errBox.textContent = (probe.ok ? "✓ " : "✗ ") + (probe.message || "");
      }
      await loadProviders();
      await loadModels();

      if (autoDetect && saved && (!probe || probe.models_ok)) {
        closeProviderModal();
        openDetectModal(saved);
      } else if (!autoDetect) {
        if (!probe || probe.ok) closeProviderModal();
      }
    } catch (e) {
      errBox.style.color = "";
      errBox.textContent = e.message || "儲存失敗，請重試";
    } finally {
      if (saveBtn) { saveBtn.disabled = false; saveBtn.textContent = "儲存並探測連通"; }
      if (saveOnlyBtn) saveOnlyBtn.disabled = false;
    }
  }

  // 预设配置快捷填充
  function applyPreset(preset) {
    const presets = {
      "gemini-official": {
        name: "Google Gemini 官方 API",
        base_url: "https://generativelanguage.googleapis.com/v1beta/openai",
      },
      "xai-grok": {
        name: "xAI Grok 官方 API",
        base_url: "https://api.x.ai/v1",
      },
      "deepseek-official": {
        name: "DeepSeek 官方 API",
        base_url: "https://api.deepseek.com/v1",
      },
      "openai-official": {
        name: "OpenAI 官方 API",
        base_url: "https://api.openai.com/v1",
      },
      "openrouter": {
        name: "OpenRouter",
        base_url: "https://openrouter.ai/api/v1",
      },
      "vultr-inference": {
        name: "Vultr Serverless Inference",
        base_url: "https://api.vultrinference.com/v1",
      },
    };
    const item = presets[preset];
    if (item) {
      el("pName").value = item.name;
      el("pBaseURL").value = item.base_url;
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 2. 模型自动探测 (Detect Models)
  // ═══════════════════════════════════════════════════════════════════════════

  async function openDetectModal(p) {
    currentDetectProvider = p;
    detectedList = [];
    el("detectSubtitle").textContent = `正在向服务商「${p.name}」(${p.base_url}) 请求 /models 端点...`;
    el("detectErr").textContent = "";
    el("detectContent").style.display = "none";
    el("detectImport").style.display = "none";
    el("detectModal").classList.add("show");

    try {
      const res = await DW.api("/api/admin/providers/detect", {
        method: "POST",
        body: JSON.stringify({ provider_id: p.id }),
      });
      detectedList = res.models || [];
      renderDetectedModels();
    } catch (e) {
      el("detectSubtitle").textContent = "探测失败";
      el("detectErr").textContent = e.message || "请求上游 /models 失败，请检查服务商 Base URL 和 Key 是否正确";
    }
  }

  function renderDetectedModels() {
    el("detectSubtitle").textContent = `服务商「${currentDetectProvider.name}」探测成功！勾选需要上架的模型后点击下方导入：`;
    el("detectCount").textContent = `共探测到 ${detectedList.length} 个模型`;
    el("detectContent").style.display = "block";
    el("detectImport").style.display = "inline-block";

    const tbody = el("detectRows");
    tbody.innerHTML = "";

    detectedList.forEach((m, idx) => {
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td style="text-align:center;">
          <input type="checkbox" class="detect-chk" data-idx="${idx}" checked />
        </td>
        <td>
          <code style="font-size:12px; color:var(--accent);">${DW.escapeHTML(m.raw_model_id || m.id)}</code>
        </td>
        <td>
          <input type="text" class="field-input detect-name" data-idx="${idx}" value="${DW.escapeHTML(m.name || m.id)}" style="padding:4px 8px; font-size:13px;" />
        </td>
        <td>
          <select class="field-input detect-vendor" data-idx="${idx}" style="padding:4px 8px; font-size:13px; width:120px;">
            <option value="grok" ${m.vendor === "grok" ? "selected" : ""}>Grok</option>
            <option value="gemini" ${m.vendor === "gemini" ? "selected" : ""}>Gemini</option>
            <option value="claude" ${m.vendor === "claude" ? "selected" : ""}>Claude</option>
            <option value="gpt" ${m.vendor === "gpt" ? "selected" : ""}>GPT</option>
            <option value="deepseek" ${m.vendor === "deepseek" ? "selected" : ""}>DeepSeek</option>
            <option value="minimax" ${m.vendor === "minimax" ? "selected" : ""}>MiniMax</option>
            <option value="kimi" ${m.vendor === "kimi" ? "selected" : ""}>Kimi</option>
            <option value="other" ${m.vendor === "other" || !m.vendor ? "selected" : ""}>其他</option>
          </select>
        </td>
        <td>
          <select class="field-input detect-tier" data-idx="${idx}" style="padding:4px 8px; font-size:13px; width:100px;">
            <option value="cheap" ${m.tier === "cheap" ? "selected" : ""}>便宜档</option>
            <option value="standard" ${m.tier === "standard" || !m.tier ? "selected" : ""}>主流档</option>
            <option value="premium" ${m.tier === "premium" ? "selected" : ""}>高阶档</option>
            <option value="pro" ${m.tier === "pro" ? "selected" : ""}>旗舰档</option>
          </select>
        </td>
      `;
      tbody.appendChild(tr);
    });
  }

  function closeDetectModal() {
    el("detectModal").classList.remove("show");
  }

  async function importDetectedModels() {
    const tbody = el("detectRows");
    const chks = tbody.querySelectorAll(".detect-chk");
    const toSave = [];

    chks.forEach((chk) => {
      if (chk.checked) {
        const idx = parseInt(chk.dataset.idx, 10);
        const original = detectedList[idx];
        const nameInput = tbody.querySelector(`.detect-name[data-idx="${idx}"]`);
        const vendorSelect = tbody.querySelector(`.detect-vendor[data-idx="${idx}"]`);
        const tierSelect = tbody.querySelector(`.detect-tier[data-idx="${idx}"]`);

        toSave.push({
          id: original.id,
          raw_model_id: original.raw_model_id || original.id,
          provider_id: currentDetectProvider.id,
          name: nameInput ? nameInput.value.trim() : original.name,
          vendor: vendorSelect ? vendorSelect.value : original.vendor,
          tier: tierSelect ? tierSelect.value : original.tier,
          description: original.description || "",
          is_enabled: true,
          sort_order: idx,
        });
      }
    });

    if (toSave.length === 0) {
      alert("请至少勾选一个要导入的模型");
      return;
    }

    try {
      await DW.api("/api/admin/models/batch-save", {
        method: "POST",
        body: JSON.stringify({
          provider_id: currentDetectProvider.id,
          models: toSave,
        }),
      });
      closeDetectModal();
      await loadProviders();
      await loadModels();
      alert(`成功导入并上架 ${toSave.length} 个模型！`);
    } catch (e) {
      alert(e.message || "导入失败");
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 3. 模型管理与前端开关 (Models Management)
  // ═══════════════════════════════════════════════════════════════════════════

  async function loadModels() {
    try {
      allModels = (await DW.api("/api/admin/models")) || [];
      renderModels();
    } catch (e) {
      console.error("load models failed", e);
    }
  }

  function renderModels() {
    const tbody = el("modelRows");
    tbody.innerHTML = "";

    const q = (el("modelSearch").value || "").trim().toLowerCase();
    const vendorFilter = el("modelVendorFilter").value;

    const filtered = allModels.filter((m) => {
      if (vendorFilter && m.vendor !== vendorFilter) return false;
      if (q) {
        const idMatch = m.id.toLowerCase().includes(q);
        const nameMatch = (m.name || "").toLowerCase().includes(q);
        const provMatch = (m.provider_name || "").toLowerCase().includes(q);
        if (!idMatch && !nameMatch && !provMatch) return false;
      }
      return true;
    });

    if (filtered.length === 0) {
      tbody.innerHTML = `<tr><td colspan="8" style="text-align:center; padding:24px; color:var(--text-dim);">
        暂无符合条件的模型。可通过上方服务商卡片点击「自动探测」批量导入。
      </td></tr>`;
      return;
    }

    filtered.sort((a, b) => Number(!!b.is_enabled) - Number(!!a.is_enabled) || (a.sort_order || 0) - (b.sort_order || 0));
    filtered.forEach((m) => {
      const tr = document.createElement("tr");
      tr.draggable = true;
      tr.dataset.modelId = m.id;
      const tierBadge = `<span class="badge-tier ${m.tier || 'standard'}">${DW.t("bill.tier." + (m.tier || "standard"))}</span>`;
      const vendorBadge = `<span class="badge-vendor">${m.vendor || 'other'}</span>`;
      const provTag = m.provider_name ? `<span style="font-size:12px; color:var(--text-dim);">${DW.escapeHTML(m.provider_name)}</span>` : '<span style="color:var(--text-faint);">全局默认</span>';

      tr.innerHTML = `
        <td style="text-align:center; cursor:grab;" title="拖动以重排">⋮⋮</td>
        <td style="text-align:center;">
          <label class="switch">
            <input type="checkbox" class="model-toggle" data-id="${m.id}" ${m.is_enabled ? "checked" : ""} />
            <span class="slider"></span>
          </label>
        </td>
        <td>
          <strong>${DW.escapeHTML(m.id)}</strong>
          ${m.raw_model_id && m.raw_model_id !== m.id ? `<br><span style="font-size:11px; color:var(--text-faint); font-family:monospace;">上游: ${DW.escapeHTML(m.raw_model_id)}</span>` : ""}
        </td>
        <td>${DW.escapeHTML(m.name || m.id)}</td>
        <td style="font-size:12px; color:var(--text-dim); max-width:240px;">${DW.escapeHTML(m.description || "—")}</td>
        <td>${provTag}</td>
        <td>${vendorBadge}</td>
        <td>${tierBadge}</td>
        <td style="white-space:nowrap;">
          <button class="btn btn-ghost btn-sm m-edit-btn" data-id="${m.id}">编辑</button>
          <button class="btn btn-ghost btn-sm m-del-btn" data-id="${m.id}" style="color:#e0674f;">删除</button>
        </td>
      `;
      tbody.appendChild(tr);
    });

    // HTML5 drag-and-drop 重排：仅在用户实际拖动 ≥1 行时才提交。
    let dragSrcId = null;
    tbody.querySelectorAll("tr[draggable]").forEach((row) => {
      row.addEventListener("dragstart", (ev) => {
        dragSrcId = row.dataset.modelId;
        ev.dataTransfer.effectAllowed = "move";
        ev.dataTransfer.setData("text/plain", dragSrcId);
        row.style.opacity = "0.4";
      });
      row.addEventListener("dragend", () => { row.style.opacity = ""; });
      row.addEventListener("dragover", (ev) => { ev.preventDefault(); ev.dataTransfer.dropEffect = "move"; });
      row.addEventListener("drop", async (ev) => {
        ev.preventDefault();
        const srcId = ev.dataTransfer.getData("text/plain") || dragSrcId;
        const dstId = row.dataset.modelId;
        if (!srcId || srcId === dstId) return;
        const ids = Array.from(tbody.querySelectorAll("tr[draggable]")).map((r) => r.dataset.modelId);
        const srcIdx = ids.indexOf(srcId);
        const dstIdx = ids.indexOf(dstId);
        if (srcIdx < 0 || dstIdx < 0) return;
        ids.splice(srcIdx, 1);
        ids.splice(dstIdx, 0, srcId);
        try {
          await DW.api("/api/admin/models/reorder", { method: "POST", body: JSON.stringify({ ids }) });
          await loadModels();
        } catch (e) {
          alert(e.message || "排序失败");
        }
      });
    });

    tbody.querySelectorAll(".model-toggle").forEach((chk) => {
      chk.onchange = async () => {
        const id = chk.dataset.id;
        try {
          await DW.api(`/api/admin/models/${encodeURIComponent(id)}/toggle`, {
            method: "POST",
            body: JSON.stringify({ is_enabled: chk.checked }),
          });
          const found = allModels.find((x) => x.id === id);
          if (found) found.is_enabled = chk.checked;
        } catch (e) {
          alert(e.message || "切换失败");
          chk.checked = !chk.checked;
        }
      };
    });

    tbody.querySelectorAll(".m-edit-btn").forEach((b) => {
      b.onclick = () => {
        const m = allModels.find((x) => x.id === b.dataset.id);
        if (m) openEditModelModal(m);
      };
    });

    tbody.querySelectorAll(".m-del-btn").forEach((b) => {
      b.onclick = async () => {
        const id = b.dataset.id;
        if (!confirm(`确定要从数据库中删除模型「${id}」吗？`)) return;
        try {
          await DW.api(`/api/admin/models/${encodeURIComponent(id)}`, { method: "DELETE" });
          await loadModels();
          await loadProviders();
        } catch (e) {
          alert(e.message || "删除失败");
        }
      };
    });
  }

  function openEditModelModal(m) {
    el("mEditErr").textContent = "";
    el("mEditID").value = m.id;
    el("mEditIDDisplay").value = m.id;
    el("mEditProviderID").value = m.provider_id || 0;
    el("mEditRawID").value = m.raw_model_id || m.id;
    el("mEditName").value = m.name || m.id;
    el("mEditVendor").value = m.vendor || "other";
    el("mEditTier").value = m.tier || "standard";
    el("mEditEnabled").checked = m.is_enabled;
    el("editModelModal").classList.add("show");
  }

  function closeEditModelModal() {
    el("editModelModal").classList.remove("show");
  }

  async function saveEditedModel() {
    const errBox = el("mEditErr");
    errBox.textContent = "";
    const id = el("mEditID").value;
    const name = el("mEditName").value.trim();
    const vendor = el("mEditVendor").value;
    const tier = el("mEditTier").value;
    const is_enabled = el("mEditEnabled").checked;
    const provider_id = parseInt(el("mEditProviderID").value, 10) || 0;
    const raw_model_id = el("mEditRawID").value.trim() || id;

    if (!name) {
      errBox.textContent = "展示名称不能为空";
      return;
    }

    try {
      await DW.api(`/api/admin/models/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify({
          name,
          vendor,
          tier,
          is_enabled,
          provider_id,
          raw_model_id,
        }),
      });
      closeEditModelModal();
      await loadModels();
    } catch (e) {
      errBox.textContent = e.message || "保存失败";
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 4. 用户管理 (Users Management)
  // ═══════════════════════════════════════════════════════════════════════════

  async function loadUsers() {
    if (!can("users.read")) return;
    allUsers = (await DW.api("/api/admin/users")) || [];
    renderUsers();
  }

  function renderUsers() {
    const tbody = el("userRows");
    if (!tbody) return;
    const q = ((el("userSearch") && el("userSearch").value) || "").trim().toLowerCase();
    tbody.innerHTML = "";
    allUsers.forEach((u) => {
      if (q && !(u.username || "").toLowerCase().includes(q)) return;
      const tr = document.createElement("tr");
      const role = userRole(u);
      let statusTag = u.is_disabled
        ? `<span class="tag off">${DW.t("admin.status.disabled")}</span>`
        : `<span class="tag ok">${DW.t("admin.status.active")}</span>`;
      if (u.is_pro) {
        statusTag += ` <span class="tag pro">${DW.t("admin.status.pro")}${u.pro_until ? " " + u.pro_until : ""}</span>`;
      }

      let roleCell = `<span class="tag role-${role}">${DW.t("admin.role." + role)}</span>`;
      const isSelf = me && u.id === me.user_id;
      if (can("users.role") && !isSelf && canActOn(u)) {
        const opts = assignableRoles()
          .map((r) => `<option value="${r}"${r === role ? " selected" : ""}>${DW.t("admin.role." + r)}</option>`)
          .join("");
        roleCell = `<select class="field-input" data-act="role" data-uid="${u.id}" data-cur="${role}" style="width:110px; padding:4px 8px; font-size:12px;">${opts}</select>`;
      }

      let actions = "";
      if (!isSelf && can("users.write") && canActOn(u)) {
        const disLabel = u.is_disabled ? DW.t("admin.action.enable") : DW.t("admin.action.disable");
        const proLabel = u.is_pro ? DW.t("admin.action.unpro") : DW.t("admin.action.setpro");
        actions =
          `<button class="btn btn-ghost btn-sm" data-act="credit" data-uid="${u.id}">${DW.t("admin.action.credit")}</button> ` +
          `<button class="btn btn-ghost btn-sm" data-act="pro" data-uid="${u.id}" data-pro="${u.is_pro ? 1 : 0}">${proLabel}</button> ` +
          `<button class="btn btn-ghost btn-sm" data-act="quota" data-uid="${u.id}" data-q="${u.quota_usd}" data-unl="${u.unlimited ? 1 : 0}">${DW.t("admin.quota.edit")}</button> ` +
          `<button class="btn btn-ghost btn-sm" data-act="pwd" data-uid="${u.id}">${DW.t("admin.action.resetpwd")}</button> ` +
          `<button class="btn btn-ghost btn-sm" data-act="dis" data-uid="${u.id}">${disLabel}</button>`;
      }

      tr.innerHTML =
        `<td><strong>${DW.escapeHTML(u.username)}</strong></td>` +
        `<td>${roleCell}</td>` +
        `<td>${u.today_messages || 0}</td>` +
        `<td>${u.total_messages}</td>` +
        `<td>${fmtUSD(u.total_spend)}</td>` +
        `<td>${fmtUSD(u.balance_usd || 0)}</td>` +
        `<td>${statusTag}</td>` +
        `<td style="white-space:nowrap;">${actions}</td>`;
      tbody.appendChild(tr);
    });

    tbody.querySelectorAll('button[data-act="dis"]').forEach((b) => {
      b.onclick = () => toggleDisable(b.dataset.uid);
    });
    tbody.querySelectorAll('button[data-act="quota"]').forEach((b) => {
      b.onclick = () => editQuota(b.dataset.uid, parseFloat(b.dataset.q), b.dataset.unl === "1");
    });
    tbody.querySelectorAll('button[data-act="credit"]').forEach((b) => {
      b.onclick = () => creditUser(b.dataset.uid);
    });
    tbody.querySelectorAll('button[data-act="pro"]').forEach((b) => {
      b.onclick = () => togglePro(b.dataset.uid, b.dataset.pro === "1");
    });
    tbody.querySelectorAll('button[data-act="pwd"]').forEach((b) => {
      b.onclick = () => resetPassword(b.dataset.uid);
    });
    tbody.querySelectorAll('select[data-act="role"]').forEach((s) => {
      s.onchange = () => setUserRole(s.dataset.uid, s.value, s.dataset.cur);
    });
  }

  async function creditUser(uid) {
    const inp = prompt(DW.t("admin.credit.prompt"), "5");
    if (inp === null) return;
    const amount = parseFloat(inp);
    if (!amount) return;
    try {
      await DW.api("/api/admin/users/" + uid + "/balance", { method: "POST", body: JSON.stringify({ amount }) });
      await loadUsers();
    } catch (e) { alert(e.message || DW.t("common.error")); }
  }

  async function togglePro(uid, isPro) {
    let days = 0;
    if (!isPro) {
      const inp = prompt(DW.t("admin.pro.prompt"), "30");
      if (inp === null) return;
      days = parseInt(inp, 10) || 0;
    } else if (!confirm(DW.t("admin.pro.confirm.off"))) {
      return;
    }
    try {
      await DW.api("/api/admin/users/" + uid + "/pro", {
        method: "POST", body: JSON.stringify({ is_pro: !isPro, days }),
      });
      await loadUsers();
    } catch (e) { alert(e.message || DW.t("common.error")); }
  }

  async function toggleDisable(uid) {
    try { await DW.api("/api/admin/users/" + uid + "/disable", { method: "POST" }); await loadUsers(); }
    catch (e) { alert(e.message || DW.t("common.error")); }
  }

  async function resetPassword(uid) {
    const inp = prompt(DW.t("admin.pwd.prompt"), "");
    if (inp === null) return;
    if (inp.length < 6) { alert(DW.t("admin.pwd.short")); return; }
    try {
      await DW.api("/api/admin/users/" + uid + "/password", { method: "POST", body: JSON.stringify({ password: inp }) });
      alert(DW.t("admin.pwd.ok"));
    } catch (e) { alert(e.message || DW.t("common.error")); }
  }

  async function setUserRole(uid, role, prev) {
    try {
      await DW.api("/api/admin/users/" + uid + "/role", { method: "POST", body: JSON.stringify({ role }) });
      await loadUsers();
    } catch (e) {
      alert(e.message || DW.t("common.error"));
      await loadUsers();
    }
  }

  async function editQuota(uid, cur, unlimited) {
    const inp = prompt(DW.t("admin.quota.edit") + " (USD, 输入 -1 表示无限)", unlimited ? "-1" : String(cur));
    if (inp === null) return;
    const val = parseFloat(inp);
    const body = val < 0 ? { quota_usd: 0, unlimited: true } : { quota_usd: val, unlimited: false };
    try { await DW.api("/api/admin/users/" + uid + "/quota", { method: "POST", body: JSON.stringify(body) }); await loadUsers(); }
    catch (e) { alert(e.message || DW.t("common.error")); }
  }

  async function createUser(e) {
    e.preventDefault();
    el("createErr").textContent = "";
    const username = el("newUsername").value.trim();
    const password = el("newPassword").value;
    const quota_usd = parseFloat(el("newQuota").value) || 0;
    const role = (el("newRole") && el("newRole").value) || "user";
    try {
      await DW.api("/api/admin/users", { method: "POST", body: JSON.stringify({ username, password, quota_usd, role }) });
      el("newUsername").value = ""; el("newPassword").value = "";
      await loadUsers(); await loadStats();
    } catch (e2) { el("createErr").textContent = e2.message || DW.t("common.error"); }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 5. 收款与计费配置
  // ═══════════════════════════════════════════════════════════════════════════
  let qrPending = { alipay: null, wechat: null };

  function fileToDataURI(file, maxPx) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = reject;
      reader.onload = () => {
        const img = new Image();
        img.onerror = reject;
        img.onload = () => {
          const scale = Math.min(1, maxPx / Math.max(img.width, img.height));
          const w = Math.round(img.width * scale), h = Math.round(img.height * scale);
          const cv = document.createElement("canvas");
          cv.width = w; cv.height = h;
          cv.getContext("2d").drawImage(img, 0, 0, w, h);
          resolve(cv.toDataURL("image/jpeg", 0.86));
        };
        img.src = reader.result;
      };
      reader.readAsDataURL(file);
    });
  }

  function showQR(slot, uri) {
    const box = el(slot);
    if (!uri) return;
    box.innerHTML = "";
    const img = document.createElement("img");
    img.src = uri;
    box.appendChild(img);
  }

  async function loadPayConfig() {
    try {
      const c = await DW.api("/api/admin/payconfig");
      showQR("alipayPreview", c.alipay_qr);
      showQR("wechatPreview", c.wechat_qr);
      el("payEmail").value = c.contact_email || "";
      el("payProPrice").value = c.pro_price;
      el("payMinTopup").value = c.min_topup;
      el("payNudge").value = c.soft_nudge_at;
      const L = c.limits || {};
      const f = L.free || {}, p = L.pro || {};
      el("limFreeCheap").value = f.cheap; el("limProCheap").value = p.cheap;
      el("limFreeStandard").value = f.standard; el("limProStandard").value = p.standard;
      el("limFreePremium").value = f.premium; el("limProPremium").value = p.premium;
      el("limFreePro").value = f.pro; el("limProPro").value = p.pro;
    } catch (e) {}
  }

  async function savePayConfig() {
    const msg = el("payMsg");
    const num = (id) => parseInt(el(id).value, 10) || 0;
    const body = {
      contact_email: el("payEmail").value,
      pro_price: parseFloat(el("payProPrice").value) || undefined,
      min_topup: parseFloat(el("payMinTopup").value) || undefined,
      soft_nudge_at: num("payNudge") || undefined,
      limits: {
        free: { cheap: num("limFreeCheap"), standard: num("limFreeStandard"), premium: num("limFreePremium"), pro: num("limFreePro") },
        pro: { cheap: num("limProCheap"), standard: num("limProStandard"), premium: num("limProPremium"), pro: num("limProPro") },
      },
    };
    if (qrPending.alipay) body.alipay_qr = qrPending.alipay;
    if (qrPending.wechat) body.wechat_qr = qrPending.wechat;
    try {
      await DW.api("/api/admin/payconfig", { method: "POST", body: JSON.stringify(body) });
      qrPending = { alipay: null, wechat: null };
      msg.style.color = "#4fae74";
      msg.textContent = DW.t("admin.provider.saved");
      await loadPayConfig();
    } catch (e) {
      msg.style.color = "#e0674f";
      msg.textContent = e.message || DW.t("common.error");
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 初始化与事件监听
  // ═══════════════════════════════════════════════════════════════════════════

  async function init() {
    me = await DW.whoami();
    applyPerms();
    if (el("userSearch")) el("userSearch").oninput = renderUsers;

    // 服务商事件
    el("addProviderBtn").onclick = () => openProviderModal(null);
    el("pModalCancel").onclick = closeProviderModal;
    el("pModalSave").onclick = () => saveProvider(true);
    if (el("pModalSaveOnly")) el("pModalSaveOnly").onclick = () => saveProvider(false);
    el("pToggleKey").onclick = () => {
      const inp = el("pAPIKey");
      inp.type = inp.type === "password" ? "text" : "password";
    };
    [el("pName"), el("pBaseURL"), el("pAPIKey")].forEach((input) => {
      input.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          saveProvider(false);
        }
      });
    });
    el("providerModal").addEventListener("click", (e) => {
      if (e.target === el("providerModal")) closeProviderModal();
    });

    document.querySelectorAll(".preset-group .preset-btn").forEach((btn) => {
      btn.onclick = () => applyPreset(btn.dataset.preset);
    });

    // 探测模态框事件
    el("detectCancel").onclick = closeDetectModal;
    el("detectImport").onclick = importDetectedModels;
    el("detectModal").addEventListener("click", (e) => {
      if (e.target === el("detectModal")) closeDetectModal();
    });
    el("editModelModal").addEventListener("click", (e) => {
      if (e.target === el("editModelModal")) closeEditModelModal();
    });
    el("detectSelectAll").onclick = () => {
      el("detectRows").querySelectorAll(".detect-chk").forEach((c) => (c.checked = true));
    };
    el("detectDeselectAll").onclick = () => {
      el("detectRows").querySelectorAll(".detect-chk").forEach((c) => (c.checked = false));
    };

    // 模型管理事件
    el("modelSearch").oninput = renderModels;
    el("modelVendorFilter").onchange = renderModels;
    el("mEditCancel").onclick = closeEditModelModal;
    el("mEditSave").onclick = saveEditedModel;

    async function applyFeatured8() {
      const res = await DW.api("/api/admin/models/featured", { method: "POST" });
      await loadModels();
      await loadProviders();
      return res;
    }
    if (el("btnFeatured8")) {
      el("btnFeatured8").onclick = async () => {
        if (!confirm("只启用精选 8 款（Vultr 7 款 + xAI Grok 1 款）？其余模型会关闭但不会删除。")) return;
        try {
          const res = await applyFeatured8();
          if (res && res.status === "ok") {
            alert("已启用精选 " + (res.enabled || 8) + " 款模型");
          } else {
            alert((res && (res.error || res.message)) || "启用失败：服务端未返回 ok");
          }
        } catch (e) {
          alert(e.message || "启用失败");
        }
      };
    }
    if (el("btnDiagnoseModels")) {
      el("btnDiagnoseModels").onclick = async () => {
        const btn = el("btnDiagnoseModels");
        const origText = btn.textContent;
        btn.disabled = true;
        btn.textContent = "⏳ 正在并发诊断全部模型...";
        try {
          const res = await DW.api("/api/admin/models/diagnose");
          const list = (res && res.models) || [];
          let passCount = 0, failCount = 0;
          list.forEach((m) => { if (m.ok) passCount++; else failCount++; });
          
          const diagMap = {};
          list.forEach((m) => { diagMap[m.id] = m; });
          
          const tbody = el("modelRows");
          tbody.querySelectorAll("tr[data-model-id]").forEach((tr) => {
            const mid = tr.dataset.modelId;
            const d = diagMap[mid];
            if (!d) return;
            const statusCell = tr.querySelector("td:nth-child(5)");
            if (statusCell) {
              const badge = d.ok
                ? `<span style="display:inline-block; padding:2px 6px; border-radius:4px; font-size:11px; background:rgba(34,197,94,0.15); color:#22c55e;">✅ 通畅 (${d.latency_ms}ms)</span>`
                : `<span style="display:inline-block; padding:2px 6px; border-radius:4px; font-size:11px; background:rgba(239,68,68,0.15); color:#ef4444;" title="${DW.escapeHTML(d.message)}">❌ 异常 (${DW.escapeHTML(d.message.slice(0, 24))})</span>`;
              const fbText = d.fallbacks && d.fallbacks.length > 0 ? `<br><span style="font-size:11px; color:var(--text-faint);">降级备选: ${d.fallbacks.slice(0, 2).join(", ")}</span>` : "";
              statusCell.innerHTML = `${badge}${fbText}`;
            }
          });

          alert(`诊断完成！共测试 ${list.length} 个模型：\n✅ 正常连通: ${passCount} 个\n❌ 异常/无渠道: ${failCount} 个\n\n系统已为所有异常模型自动启用多级智能降级兜底！`);
        } catch (e) {
          alert(e.message || "诊断请求失败");
        } finally {
          btn.disabled = false;
          btn.textContent = origText;
        }
      };
    }
    if (el("detectFeatured")) {
      el("detectFeatured").onclick = async () => {
        try {
          const res = await applyFeatured8();
          closeDetectModal();
          if (res && res.status === "ok") {
            alert("已启用精选 " + (res.enabled || 8) + " 款模型");
          } else {
            alert((res && (res.error || res.message)) || "启用失败：服务端未返回 ok");
          }
        } catch (e) {
          alert(e.message || "启用失败");
        }
      };
    }

    el("btnEnableAll").onclick = async () => {
      if (!confirm("确定要在用户端启用当前筛选出的所有模型吗？")) return;
      for (const m of allModels) {
        if (!m.is_enabled) {
          try {
            await DW.api(`/api/admin/models/${encodeURIComponent(m.id)}/toggle`, {
              method: "POST",
              body: JSON.stringify({ is_enabled: true }),
            });
            m.is_enabled = true;
          } catch (e) {}
        }
      }
      renderModels();
    };

    el("btnDisableAll").onclick = async () => {
      if (!confirm("确定要在用户端停用当前筛选出的所有模型吗？")) return;
      for (const m of allModels) {
        if (m.is_enabled) {
          try {
            await DW.api(`/api/admin/models/${encodeURIComponent(m.id)}/toggle`, {
              method: "POST",
              body: JSON.stringify({ is_enabled: false }),
            });
            m.is_enabled = false;
          } catch (e) {}
        }
      }
      renderModels();
    };

    // 收款与用户管理事件
    el("paySave").onclick = savePayConfig;
    el("createForm").addEventListener("submit", createUser);
    el("alipayFile").onchange = async (e) => {
      if (!e.target.files[0]) return;
      qrPending.alipay = await fileToDataURI(e.target.files[0], 420);
      showQR("alipayPreview", qrPending.alipay);
    };
    el("wechatFile").onchange = async (e) => {
      if (!e.target.files[0]) return;
      qrPending.wechat = await fileToDataURI(e.target.files[0], 420);
      showQR("wechatPreview", qrPending.wechat);
    };

    const jobs = [];
    if (can("system.read")) {
      jobs.push(loadSystemStatus());
      setInterval(loadSystemStatus, 5000);
    }
    if (can("stats.read")) jobs.push(loadStats());
    if (can("providers.manage")) jobs.push(loadProviders());
    if (can("models.manage")) jobs.push(loadModels());
    if (can("config.manage")) jobs.push(loadPayConfig());
    if (can("users.read")) jobs.push(loadUsers());
    await Promise.allSettled(jobs);
  }

  async function loadSystemStatus() {
    try {
      const s = await DW.api("/api/admin/system");
      if (!s) return;
      if (el("vpsCPU")) el("vpsCPU").textContent = (s.cpu_percent || 0) + "%";
      if (el("vpsCPUSub")) el("vpsCPUSub").textContent = `${s.cpu_count || 1} 核心 (${s.os || "Linux"}/${s.arch || "amd64"})`;

      if (el("vpsRAM")) el("vpsRAM").textContent = (s.mem_percent || 0) + "%";
      if (el("vpsRAMSub")) el("vpsRAMSub").textContent = `${s.mem_used_mb || 0} / ${s.mem_total_mb || 0} MB`;

      if (el("vpsDisk")) el("vpsDisk").textContent = (s.disk_percent || 0) + "%";
      if (el("vpsDiskSub")) el("vpsDiskSub").textContent = `${s.disk_used_gb || 0} / ${s.disk_total_gb || 0} GB`;

      if (el("vpsUptime")) el("vpsUptime").textContent = s.uptime_str || "—";
      if (el("vpsLoad")) el("vpsLoad").textContent = `負載: ${s.load_avg || "—"} · App: ${s.app_alloc_mb || 0}MB`;

      if (el("vpsRefreshTime")) el("vpsRefreshTime").textContent = `更新於 ${new Date().toLocaleTimeString()}`;
    } catch (e) {
      console.warn("loadSystemStatus error", e);
    }
  }

  // 日志查看弹窗：拉一次 /api/admin/logs?lines=200，渲染到 <pre>
  async function openLogsModal() {
    const modal = el("logsModal");
    const body = el("logsBody");
    const meta = el("logsMeta");
    if (!modal || !body) return;
    modal.style.display = "flex";
    body.textContent = "加載中…";
    meta.textContent = "";
    try {
      const r = await DW.api("/api/admin/logs?lines=200");
      if (!r || r.status !== "ok") {
        body.textContent = "(無日誌或讀取失敗)";
        return;
      }
      body.textContent = (r.lines && r.lines.length) ? r.lines.join("\n") : "(空)";
      meta.textContent = `來源: ${r.source || "未知"} · 共 ${r.count || 0} 行`;
    } catch (e) {
      body.textContent = "讀取失敗：" + (e.message || "");
    }
  }
  if (el("btnViewLogs")) el("btnViewLogs").onclick = openLogsModal;
  if (el("logsClose")) el("logsClose").onclick = () => { if (el("logsModal")) el("logsModal").style.display = "none"; };
  if (el("logsModal")) el("logsModal").addEventListener("click", (ev) => {
    if (ev.target === el("logsModal")) el("logsModal").style.display = "none";
  });

  if (window.DW && typeof window.DW.isReady === "function" && window.DW.isReady()) {
    init();
  } else {
    document.addEventListener("dw:ready", init);
  }

  document.addEventListener("dw:langchange", () => {
    loadUsers();
    renderProviders();
    renderModels();
  });
})();
