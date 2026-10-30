// ── Deepweay 充值页：套餐/额度/倒计时/收款码 ────────────────────────────────
(function () {
  const el = (id) => document.getElementById(id);
  let resetAt = null;

  const TIER_LABEL = {
    cheap: "bill.tier.cheap",
    standard: "bill.tier.standard",
    premium: "bill.tier.premium",
    pro: "bill.tier.pro",
  };
  const TIER_MODELS = {
    cheap: "DeepSeek · MiniMax · Kimi · Gemini Flash",
    standard: "Grok · Gemini Pro · GPT-5.5",
    premium: "Claude Sonnet · Haiku",
    pro: "Claude Opus 4.8 · GPT-5.6 Sol",
  };

  function fmtUSD(n) { return "$" + (n || 0).toFixed(2); }

  function renderCountdown() {
    if (!resetAt) return;
    const ms = resetAt - Date.now();
    if (ms <= 0) { el("countdown").textContent = "00:00:00"; return; }
    const h = Math.floor(ms / 3600000);
    const m = Math.floor((ms % 3600000) / 60000);
    const s = Math.floor((ms % 60000) / 1000);
    el("countdown").textContent =
      String(h).padStart(2, "0") + ":" + String(m).padStart(2, "0") + ":" + String(s).padStart(2, "0");
  }

  function renderTiers(tiers) {
    const tbody = el("tierRows");
    tbody.innerHTML = "";
    ["cheap", "standard", "premium", "pro"].forEach((t) => {
      const d = tiers[t];
      if (!d) return;
      const tr = document.createElement("tr");
      const limitCell = d.locked
        ? `<span class="tag off">${DW.t("bill.tier.prolocked")}</span>`
        : d.limit;
      tr.innerHTML =
        `<td><strong>${DW.t(TIER_LABEL[t])}</strong></td>` +
        `<td style="color:var(--text-faint); font-size:12.5px;">${TIER_MODELS[t]}</td>` +
        `<td>${d.used}</td>` +
        `<td>${limitCell}</td>`;
      tbody.appendChild(tr);
    });
  }

  function renderQR(box, dataURI) {
    if (!dataURI) return;
    box.innerHTML = "";
    const img = document.createElement("img");
    img.src = dataURI;
    img.alt = "";
    box.appendChild(img);
  }

  async function init() {
    let d;
    try { d = await DW.api("/api/billing/info"); } catch (e) { return; }

    const planKey = d.plan === "pro" ? "bill.plan.pro" : d.plan === "unlimited" ? "bill.plan.unlimited" : "bill.plan.free";
    el("planName").textContent = DW.t(planKey);
    el("todayCount").textContent = d.today_messages || 0;
    el("balance").textContent = fmtUSD(d.balance_usd);
    el("proPrice").textContent = d.pro_price;
    el("minTopup").textContent = d.min_topup;

    renderTiers(d.tiers || {});

    resetAt = new Date(d.next_reset).getTime();
    renderCountdown();
    setInterval(renderCountdown, 1000);

    const pay = d.pay || {};
    renderQR(el("alipayBox"), pay.alipay_qr);
    renderQR(el("wechatBox"), pay.wechat_qr);
    if (!pay.googlepay_on) el("gpayCard").classList.add("dim");

    const mail = d.contact_email || "ops@example.com";
    const a = el("contactEmail");
    a.textContent = mail;
    a.href = "mailto:" + mail + "?subject=" + encodeURIComponent("multi-llm-chat 充值开通");
  }

  document.addEventListener("dw:ready", init);
})();
