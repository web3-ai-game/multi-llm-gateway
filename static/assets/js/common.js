// ── Deepweay 前端公共库：i18n + 主题 + API + 导航 ──────────────────────────
(function () {
  const LANGS = ["zh-Hant", "zh-Hans", "en"];
  const LANG_LABEL = { "zh-Hant": "繁", "zh-Hans": "简", en: "EN" };
  const DEFAULT_LANG = "zh-Hant";
  const DEFAULT_THEME = "black";

  // ── cookie helpers ──
  function setCookie(k, v) {
    document.cookie = `${k}=${encodeURIComponent(v)};path=/;max-age=31536000;samesite=lax`;
  }
  function getCookie(k) {
    const m = document.cookie.match(new RegExp("(?:^|; )" + k + "=([^;]*)"));
    return m ? decodeURIComponent(m[1]) : "";
  }

  // ── theme（防闪烁的同步设置见各页 <head> 内联脚本）──
  function currentTheme() {
    return document.documentElement.getAttribute("data-theme") || DEFAULT_THEME;
  }
  function setTheme(t) {
    document.documentElement.setAttribute("data-theme", t);
    localStorage.setItem("dw_theme", t);
    setCookie("dw_theme", t);
  }
  function toggleTheme() {
    const next = currentTheme() === "black" ? "cream" : "black";
    document.body.classList.add("theme-flip");
    setTheme(next);
    setTimeout(() => document.body.classList.remove("theme-flip"), 360);
  }

  // ── i18n ──
  let dict = {};
  function resolveLang() {
    const q = getCookie("dw_lang") || localStorage.getItem("dw_lang");
    if (q && LANGS.includes(q)) return q;
    const nav = (navigator.language || "").toLowerCase();
    if (nav.startsWith("zh")) {
      if (nav.includes("tw") || nav.includes("hk") || nav.includes("hant")) return "zh-Hant";
      return "zh-Hans";
    }
    if (nav.startsWith("en")) return "en";
    return DEFAULT_LANG;
  }
  let curLang = resolveLang();

  const I18N_VER = "11";
  const A2HS_KEY = "dw_a2hs_v1";
  async function loadDict(lang) {
    const res = await fetch(`/i18n/${lang}.json?v=${I18N_VER}`);
    dict = await res.json();
  }
  function t(key) {
    return dict[key] || key;
  }
  function applyI18n(root) {
    (root || document).querySelectorAll("[data-i18n]").forEach((el) => {
      el.textContent = t(el.getAttribute("data-i18n"));
    });
    (root || document).querySelectorAll("[data-i18n-ph]").forEach((el) => {
      el.setAttribute("placeholder", t(el.getAttribute("data-i18n-ph")));
    });
    document.documentElement.lang = curLang;
  }
  async function setLang(lang) {
    if (!LANGS.includes(lang)) return;
    curLang = lang;
    localStorage.setItem("dw_lang", lang);
    setCookie("dw_lang", lang);
    await loadDict(lang);
    applyI18n();
    renderLangSwitch();
    document.dispatchEvent(new CustomEvent("dw:langchange"));
  }

  // ── build the theme+lang control cluster ──
  function renderLangSwitch() {
    document.querySelectorAll(".lang-switch button").forEach((b) => {
      b.classList.toggle("active", b.dataset.lang === curLang);
    });
  }
  function mountControls(container) {
    if (!container) return;
    const lang = document.createElement("div");
    lang.className = "lang-switch";
    LANGS.forEach((l) => {
      const b = document.createElement("button");
      b.dataset.lang = l;
      b.textContent = LANG_LABEL[l];
      b.onclick = () => setLang(l);
      lang.appendChild(b);
    });
    const theme = document.createElement("button");
    theme.className = "theme-toggle";
    theme.setAttribute("aria-label", "toggle theme");
    theme.onclick = toggleTheme;
    container.appendChild(lang);
    container.appendChild(theme);
    renderLangSwitch();
  }

  const TOKEN_KEY = "dw_token";

  function authToken() {
    try { return localStorage.getItem(TOKEN_KEY) || ""; } catch (e) { return ""; }
  }

  function rememberAuth(data) {
    if (!data || !data.token) return;
    try { localStorage.setItem(TOKEN_KEY, data.token); } catch (e) {}
  }

  function clearAuth() {
    try { localStorage.removeItem(TOKEN_KEY); } catch (e) {}
  }

  // ── API helper ──
  // 微信內建瀏覽器會丟掉跨 Worker 的 Set-Cookie，所以登入態同時放 localStorage。
  async function api(path, opts = {}) {
    const { skipAuthRedirect, ...fetchOpts } = opts;
    const headers = { "Content-Type": "application/json", ...(fetchOpts.headers || {}) };
    const token = authToken();
    if (token && !headers.Authorization) headers.Authorization = "Bearer " + token;
    const res = await fetch(path, {
      credentials: "same-origin",
      ...fetchOpts,
      headers,
    });
    const isAuthForm = path === "/api/login" || path === "/api/register";
    if (res.status === 401 && !skipAuthRedirect && !isAuthForm) {
      window.location.href = "/login?next=" + encodeURIComponent(location.pathname);
      throw new Error("unauthorized");
    }
    let data = null;
    try { data = await res.json(); } catch (e) {}
    if (!res.ok) throw new Error((data && data.error) || "request failed");
    if (path === "/api/login" || path === "/api/register") rememberAuth(data);
    return data;
  }

  // whoami 不重定向：未登录返回 null（供公开页判断登录态）。
  let _me = undefined;
  async function whoami(force) {
    if (_me !== undefined && !force) return _me;
    try {
      const headers = { "Content-Type": "application/json" };
      const token = authToken();
      if (token) headers.Authorization = "Bearer " + token;
      const res = await fetch("/api/whoami", { headers, credentials: "same-origin" });
      _me = res.ok ? await res.json() : null;
    } catch (e) { _me = null; }
    return _me;
  }

  async function logout() {
    clearAuth();
    try { await fetch("/api/logout", { method: "POST", credentials: "same-origin" }); } catch (e) {}
    window.location.href = "/";
  }

  // ── 右上角统一用户菜单：未登录→登入+註冊按钮；已登录→用户名下拉（自动识别 admin）──
  async function mountUserMenu(container) {
    if (!container) return;
    const me = await whoami();
    const nab = document.getElementById("navAuthBtns");
    container.innerHTML = "";
    if (!me) {
      if (nab) nab.style.display = "";
      return;
    }
    if (nab) nab.style.display = "none";
    const wrap = document.createElement("div");
    wrap.className = "user-menu";
    const links = [
      { href: "/chat", key: "nav.chat" },
      { href: "/settings", key: "nav.settings" },
      { href: "/dashboard", key: "nav.dashboard" },
    ];
    if (me.is_admin) links.push({ href: "/admin", key: "nav.admin" });
    const items = links
      .map((l) => `<a href="${l.href}" data-i18n="${l.key}">${t(l.key)}</a>`)
      .join("");
    wrap.innerHTML =
      `<button class="user-btn" type="button">` +
      `<span class="user-avatar">${escapeHTML((me.username || "?")[0].toUpperCase())}</span>` +
      `<span class="user-name">${escapeHTML(me.username)}</span>` +
      (me.role && me.role !== "user"
        ? `<span class="tag role-${me.role}" data-i18n="admin.role.${me.role}">${t("admin.role." + me.role)}</span>`
        : (me.is_admin ? `<span class="tag admin" data-i18n="admin.status.admin">${t("admin.status.admin")}</span>` : "")) +
      `<span class="caret">▾</span></button>` +
      `<div class="user-dropdown">${items}<a href="#" class="logout-link" data-i18n="nav.logout">${t("nav.logout")}</a></div>`;
    container.appendChild(wrap);
    const btn = wrap.querySelector(".user-btn");
    btn.onclick = (e) => { e.stopPropagation(); wrap.classList.toggle("open"); };
    document.addEventListener("click", () => wrap.classList.remove("open"));
    wrap.querySelector(".logout-link").onclick = (e) => { e.preventDefault(); logout(); };
  }

  let _ready = false;
  // ── init: load dict then apply ──
  async function init() {
    await loadDict(curLang);
    applyI18n();
    document.querySelectorAll("[data-controls]").forEach(mountControls);
    await Promise.all([...document.querySelectorAll("[data-usermenu]")].map(mountUserMenu));
    applyI18n();
    const me = await whoami();
    if (me) maybeShowA2HS();
    _ready = true;
    document.dispatchEvent(new CustomEvent("dw:ready"));
  }

  function isStandaloneApp() {
    return window.matchMedia("(display-mode: standalone)").matches || window.navigator.standalone === true;
  }

  function a2hsProfile() {
    const ua = navigator.userAgent || "";
    const ios = /iPhone|iPad|iPod/i.test(ua);
    const android = /Android/i.test(ua);
    const crios = /CriOS/i.test(ua);
    const samsung = /SamsungBrowser/i.test(ua);
    const w = Math.min(screen.width, screen.height);
    const h = Math.max(screen.width, screen.height);
    const iphone7p = ios && ((w === 414 && h === 736) || (w === 414 && h === 628));
    let kind = "other";
    if (ios && crios) kind = "ios-chrome";
    else if (ios) kind = "ios-safari";
    else if (android && samsung) kind = "android-samsung";
    else if (android) kind = "android-chrome";
    return { ios, android, kind, iphone7p, mobile: ios || android || window.matchMedia("(pointer: coarse)").matches };
  }

  function a2hsSteps(kind) {
    if (kind === "ios-safari" || kind === "ios-chrome") {
      return [
        { n: "1", t: t("a2hs.ios.1") },
        { n: "2", t: t("a2hs.ios.2") },
        { n: "3", t: t("a2hs.ios.3") },
      ];
    }
    if (kind === "android-samsung") {
      return [
        { n: "1", t: t("a2hs.and.1") },
        { n: "2", t: t("a2hs.ss.2") },
        { n: "3", t: t("a2hs.and.3") },
      ];
    }
    return [
      { n: "1", t: t("a2hs.and.1") },
      { n: "2", t: t("a2hs.and.2") },
      { n: "3", t: t("a2hs.and.3") },
    ];
  }

  let deferredInstall = null;
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferredInstall = e;
  });

  function dismissA2HS() {
    try { localStorage.setItem(A2HS_KEY, "1"); } catch (e) {}
    const el = document.getElementById("a2hsOverlay");
    if (el) el.remove();
  }

  function maybeShowA2HS() {
    if (isStandaloneApp()) return;
    if (location.pathname !== "/chat") return;
    try { if (localStorage.getItem(A2HS_KEY) === "1") return; } catch (e) {}
    const p = a2hsProfile();
    if (!p.mobile) return;
    const steps = a2hsSteps(p.kind);
    const title = p.ios ? t("a2hs.title.ios") : t("a2hs.title.and");
    const sub = p.iphone7p ? t("a2hs.sub.7p") : t("a2hs.sub");
    const overlay = document.createElement("div");
    overlay.id = "a2hsOverlay";
    overlay.className = "a2hs-overlay";
    overlay.innerHTML =
      `<div class="a2hs-card" role="dialog" aria-labelledby="a2hsTitle">` +
      `<button type="button" class="a2hs-x" aria-label="close">×</button>` +
      `<div class="a2hs-phone" aria-hidden="true">` +
      `<div class="a2hs-notch"></div>` +
      `<div class="a2hs-screen"><img src="/apple-touch-icon.png" alt="" /><span>multi-llm-chat</span></div>` +
      `</div>` +
      `<h3 id="a2hsTitle">${escapeHTML(title)}</h3>` +
      `<p class="a2hs-sub">${escapeHTML(sub)}</p>` +
      `<ol class="a2hs-steps">` +
      steps.map((s) => `<li><span>${s.n}</span><b>${escapeHTML(s.t)}</b></li>`).join("") +
      `</ol>` +
      `<div class="a2hs-actions">` +
      (deferredInstall ? `<button type="button" class="btn btn-primary a2hs-install">${escapeHTML(t("a2hs.install"))}</button>` : "") +
      `<button type="button" class="btn btn-ghost a2hs-ok">${escapeHTML(t("a2hs.gotit"))}</button>` +
      `</div></div>`;
    document.body.appendChild(overlay);
    overlay.querySelector(".a2hs-x").onclick = dismissA2HS;
    overlay.querySelector(".a2hs-ok").onclick = dismissA2HS;
    overlay.addEventListener("click", (e) => { if (e.target === overlay) dismissA2HS(); });
    const inst = overlay.querySelector(".a2hs-install");
    if (inst && deferredInstall) {
      inst.onclick = async () => {
        try { await deferredInstall.prompt(); } catch (e) {}
        dismissA2HS();
      };
    }
  }

  window.DW = {
    t, api, whoami, logout, setLang, toggleTheme, applyI18n, mountControls, mountUserMenu,
    lang: () => curLang, escapeHTML, isReady: () => _ready,
  };

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
