package ui

import (
	"html/template"
	"net/http"
)

type PageData struct {
	AuthEnabled  bool
	AuthRequired bool
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>zip-forger</title>
  <link rel="preconnect" href="https://fonts.googleapis.com" />
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
  <link href="https://fonts.googleapis.com/css2?family=DM+Sans:ital,opsz,wght@0,9..40,300..700;1,9..40,300..700&family=Fraunces:opsz,wght@9..144,400..700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet" />
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f1e4;
      --bg-accent: #ece3cf;
      --surface: rgba(255, 255, 255, 0.82);
      --surface-alt: rgba(255, 255, 255, 0.56);
      --text: #1a2722;
      --text-secondary: #3d4f48;
      --muted: #6b7d76;
      --line: rgba(22, 35, 31, 0.15);
      --line-strong: rgba(22, 35, 31, 0.28);
      --brand: #2f7352;
      --brand-strong: #1e4f39;
      --brand-soft: rgba(47, 115, 82, 0.08);
      --warn: #9f6324;
      --danger: #9e2f3a;
      --danger-soft: rgba(158, 47, 58, 0.08);
      --shadow: 0 1px 3px rgba(30, 45, 38, 0.06), 0 8px 32px rgba(30, 45, 38, 0.1);
      --shadow-lg: 0 4px 12px rgba(30, 45, 38, 0.08), 0 20px 48px rgba(30, 45, 38, 0.14);
      --radius: 12px;
      --radius-sm: 8px;
    }

    :root[data-theme="dark"] {
      color-scheme: dark;
      --bg: #0f1613;
      --bg-accent: #1a2420;
      --surface: rgba(28, 40, 35, 0.85);
      --surface-alt: rgba(22, 32, 28, 0.65);
      --text: #e8f0ed;
      --text-secondary: #bfcdc7;
      --muted: #8a9e96;
      --line: rgba(202, 223, 216, 0.14);
      --line-strong: rgba(202, 223, 216, 0.25);
      --brand: #58ad84;
      --brand-strong: #7bc39d;
      --brand-soft: rgba(88, 173, 132, 0.1);
      --warn: #e2a765;
      --danger: #f48e9a;
      --danger-soft: rgba(244, 142, 154, 0.1);
      --shadow: 0 1px 3px rgba(0, 0, 0, 0.12), 0 8px 32px rgba(0, 0, 0, 0.28);
      --shadow-lg: 0 4px 12px rgba(0, 0, 0, 0.16), 0 20px 48px rgba(0, 0, 0, 0.36);
    }

    * { box-sizing: border-box; margin: 0; }

    body {
      font-family: "DM Sans", "Segoe UI", system-ui, sans-serif;
      font-size: 14px;
      color: var(--text);
      background: var(--bg);
      min-height: 100vh;
    }

    .frame {
      max-width: 1280px;
      margin: 0 auto;
      padding: 20px 20px 48px;
      animation: reveal 280ms ease-out;
    }

    /* ---- Header ---- */
    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      flex-wrap: wrap;
      margin-bottom: 16px;
    }

    .header-left {
      display: flex;
      align-items: center;
      gap: 14px;
      flex-wrap: wrap;
    }

    .title {
      font-family: "Fraunces", Georgia, serif;
      font-size: 1.6rem;
      font-weight: 600;
      line-height: 1;
      color: var(--brand-strong);
    }

    :root[data-theme="dark"] .title { color: var(--brand); }

    .header-right {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .auth-badge {
      font-size: 0.78rem;
      color: var(--muted);
      padding: 4px 10px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--surface-alt);
      white-space: nowrap;
    }

    .auth-banner {
      display: none;
      padding: 10px 14px;
      border-radius: var(--radius-sm);
      background: var(--danger-soft);
      border: 1px solid var(--danger);
      color: var(--danger);
      font-size: 0.88rem;
      margin-bottom: 12px;
      align-items: center;
      gap: 8px;
    }

    .auth-banner.visible { display: flex; }

    .theme-toggle {
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--surface-alt);
      padding: 3px;
      display: inline-flex;
      gap: 2px;
    }

    .theme-btn {
      border: 0;
      border-radius: 999px;
      background: transparent;
      color: var(--muted);
      padding: 4px 9px;
      cursor: pointer;
      font-size: 0.76rem;
      font-family: inherit;
      transition: background 120ms, color 120ms;
    }

    .theme-btn.active {
      color: #fff;
      background: var(--brand);
    }

    /* ---- Tabs ---- */
    .tab-bar {
      display: flex;
      gap: 2px;
      border-bottom: 1px solid var(--line);
      margin-bottom: 16px;
    }

    .tab-btn {
      border: 0;
      border-bottom: 2px solid transparent;
      border-radius: 0;
      background: transparent;
      color: var(--muted);
      padding: 10px 18px;
      cursor: pointer;
      font-family: inherit;
      font-size: 0.92rem;
      font-weight: 500;
      transition: color 120ms, border-color 120ms;
      min-height: auto;
    }

    .tab-btn:hover {
      color: var(--text-secondary);
      transform: none;
      filter: none;
    }

    .tab-btn.active {
      color: var(--brand-strong);
      border-bottom-color: var(--brand);
    }

    :root[data-theme="dark"] .tab-btn.active {
      color: var(--brand);
    }

    .tab-panel { display: none; }
    .tab-panel.active { display: block; }

    /* ---- Cards ---- */
    .card {
      border: 1px solid var(--line);
      border-radius: var(--radius);
      background: var(--surface);
      box-shadow: var(--shadow);
      padding: 16px;
      backdrop-filter: blur(6px);
    }

    .card-title {
      font-size: 0.82rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      color: var(--muted);
      margin-bottom: 12px;
    }

    /* ---- Layout ---- */
    .dl-layout {
      display: grid;
      grid-template-columns: 380px 1fr;
      gap: 16px;
      align-items: start;
    }

    .dl-sidebar {
      display: grid;
      gap: 12px;
      position: sticky;
      top: 20px;
    }

    .dl-main {
      display: grid;
      gap: 12px;
    }

    .grid { display: grid; gap: 10px; }

    .row-2 {
      display: grid;
      gap: 10px;
      grid-template-columns: 1fr 1fr;
    }

    .row-3 {
      display: grid;
      gap: 10px;
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }

    /* ---- Forms ---- */
    label {
      display: grid;
      gap: 4px;
      font-size: 0.82rem;
      font-weight: 500;
      color: var(--muted);
    }

    input, select, textarea, button { font: inherit; }

    input, select, textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: var(--radius-sm);
      padding: 7px 10px;
      background: rgba(255, 255, 255, 0.7);
      color: var(--text);
      outline: none;
      font-size: 0.88rem;
      transition: border-color 100ms, box-shadow 100ms;
    }

    :root[data-theme="dark"] input,
    :root[data-theme="dark"] select,
    :root[data-theme="dark"] textarea {
      background: rgba(12, 18, 16, 0.75);
    }

    input:focus, select:focus, textarea:focus {
      border-color: var(--brand);
      box-shadow: 0 0 0 2px rgba(88, 173, 132, 0.18);
    }

    textarea {
      min-height: 62px;
      resize: vertical;
      line-height: 1.4;
    }

    .check {
      display: flex;
      align-items: center;
      gap: 7px;
      font-size: 0.86rem;
      color: var(--muted);
    }

    .check input { width: auto; margin: 0; }

    /* ---- Buttons ---- */
    button, .btn {
      border: 1px solid transparent;
      border-radius: var(--radius-sm);
      padding: 7px 14px;
      cursor: pointer;
      text-decoration: none;
      font-weight: 500;
      font-size: 0.86rem;
      transition: transform 80ms, filter 80ms, opacity 80ms;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      min-height: 34px;
    }

    button:hover:not(:disabled), .btn:hover:not(:disabled) {
      transform: translateY(-1px);
      filter: brightness(1.04);
    }

    button:disabled, .btn:disabled {
      opacity: 0.55;
      cursor: not-allowed;
      transform: none;
      filter: none;
    }

    .primary {
      color: #fff;
      background: var(--brand);
    }

    .ghost {
      color: var(--brand-strong);
      background: var(--surface-alt);
      border-color: var(--line);
    }

    :root[data-theme="dark"] .ghost { color: var(--brand); }

    .warn-btn {
      color: #fff;
      background: var(--warn);
    }

    .danger-btn {
      color: #fff;
      background: var(--danger);
      font-size: 0.8rem;
      padding: 4px 10px;
      min-height: 28px;
    }

    .btn-row {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
    }

    .btn-download {
      color: #fff;
      background: var(--brand-strong);
      font-size: 0.92rem;
      padding: 9px 20px;
      min-height: 38px;
      border-radius: var(--radius-sm);
      font-weight: 600;
      letter-spacing: 0.2px;
    }

    :root[data-theme="dark"] .btn-download { background: var(--brand); color: #0f1613; }

    /* ---- Message ---- */
    .message {
      min-height: 1rem;
      color: var(--muted);
      font-size: 0.86rem;
    }

    .message.ok { color: var(--brand-strong); }
    :root[data-theme="dark"] .message.ok { color: var(--brand); }
    .message.warn { color: var(--warn); }
    .message.err { color: var(--danger); }

    .hint {
      color: var(--muted);
      font-size: 0.82rem;
    }

    .small {
      font-size: 0.8rem;
      color: var(--muted);
    }

    /* ---- Stats bar ---- */
    .stats-bar {
      display: flex;
      gap: 16px;
      align-items: baseline;
      flex-wrap: wrap;
      padding: 8px 0;
      font-size: 0.84rem;
    }

    .stat-item {
      display: flex;
      gap: 5px;
      align-items: baseline;
    }

    .stat-label {
      color: var(--muted);
      text-transform: uppercase;
      font-size: 0.72rem;
      letter-spacing: 0.4px;
      font-weight: 600;
    }

    .stat-value {
      color: var(--text-secondary);
      font-family: "JetBrains Mono", monospace;
      font-size: 0.82rem;
    }

    /* ---- Tree ---- */
    .tree {
      border: 1px solid var(--line);
      border-radius: var(--radius-sm);
      background: var(--surface-alt);
      min-height: 300px;
      max-height: calc(100vh - 320px);
      overflow: auto;
      padding: 12px 14px;
      font-family: "JetBrains Mono", "Cascadia Mono", monospace;
      font-size: 0.78rem;
      line-height: 1.55;
      white-space: pre;
      color: var(--text-secondary);
    }

    .tree-empty {
      color: var(--muted);
      font-family: "DM Sans", sans-serif;
      font-style: italic;
      font-size: 0.86rem;
      white-space: normal;
      padding: 32px 16px;
      text-align: center;
    }

    /* ---- Share row ---- */
    .share-row {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 6px;
      align-items: end;
    }

    /* ---- Preview header ---- */
    .preview-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
      margin-bottom: 8px;
    }

    /* ---- Config editor ---- */
    .config-layout {
      max-width: 720px;
    }

    .preset-list {
      display: grid;
      gap: 10px;
      margin-top: 10px;
    }

    .preset {
      border: 1px solid var(--line);
      border-radius: var(--radius-sm);
      background: var(--surface-alt);
      padding: 12px;
      display: grid;
      gap: 8px;
    }

    .preset-head {
      display: flex;
      gap: 8px;
      align-items: center;
      justify-content: space-between;
    }

    .preset-head strong {
      font-size: 0.88rem;
      font-weight: 600;
      color: var(--text);
    }

    /* ---- Responsive ---- */
    @media (max-width: 840px) {
      .dl-layout { grid-template-columns: 1fr; }
      .dl-sidebar { position: static; }
      .row-2, .row-3 { grid-template-columns: 1fr; }
      .share-row { grid-template-columns: 1fr; }
      .tree { max-height: 50vh; }
    }

    @keyframes reveal {
      from { opacity: 0; transform: translateY(6px); }
      to { opacity: 1; transform: translateY(0); }
    }

    @keyframes spin {
      to { transform: rotate(360deg); }
    }

    .spinner {
      display: inline-block;
      width: 14px;
      height: 14px;
      border: 2px solid rgba(255,255,255,0.3);
      border-top-color: #fff;
      border-radius: 50%;
      animation: spin 600ms linear infinite;
      vertical-align: middle;
    }

    .spinner-dark {
      border-color: rgba(0,0,0,0.15);
      border-top-color: var(--brand);
    }
  </style>
</head>
<body>
  <main class="frame">
    <header class="header">
      <div class="header-left">
        <h1 class="title">zip-forger</h1>
        <span class="auth-badge" id="authBadge">checking...</span>
        <a class="btn ghost" id="loginBtn" href="/auth/login?return_to=/" hidden style="font-size:0.8rem;padding:4px 10px;min-height:26px;">Sign in</a>
        <button class="ghost" id="logoutBtn" type="button" hidden style="font-size:0.8rem;padding:4px 10px;min-height:26px;">Sign out</button>
      </div>
      <div class="header-right">
        <div class="theme-toggle" role="group" aria-label="theme mode">
          <button class="theme-btn" type="button" data-theme="system">Sys</button>
          <button class="theme-btn" type="button" data-theme="light">Light</button>
          <button class="theme-btn" type="button" data-theme="dark">Dark</button>
        </div>
      </div>
    </header>

    <div class="auth-banner" id="authBanner">
      Authentication is required to use this service. Please sign in.
      <a class="btn ghost" id="bannerLoginBtn" href="/auth/login?return_to=/" style="font-size:0.8rem;padding:4px 10px;min-height:26px;margin-left:auto;">Sign in</a>
    </div>

    <nav class="tab-bar" role="tablist">
      <button class="tab-btn active" type="button" role="tab" data-tab="download" aria-selected="true">Download</button>
      <button class="tab-btn" type="button" role="tab" data-tab="configure" aria-selected="false">Configure</button>
    </nav>

    <!-- ==================== DOWNLOAD TAB ==================== -->
    <div class="tab-panel active" id="panel-download" role="tabpanel">
      <div class="dl-layout">
        <div class="dl-sidebar">
          <section class="card">
            <div class="card-title">Repository</div>
            <div class="grid">
              <div class="row-3">
                <label>Owner
                  <input id="owner" list="ownerOptions" autocomplete="off" placeholder="owner" />
                  <datalist id="ownerOptions"></datalist>
                </label>
                <label>Repo
                  <input id="repo" list="repoOptions" autocomplete="off" placeholder="repo" />
                  <datalist id="repoOptions"></datalist>
                </label>
                <label>Ref
                  <input id="ref" list="branchOptions" value="main" autocomplete="off" placeholder="main" />
                  <datalist id="branchOptions"></datalist>
                </label>
              </div>
              <button class="ghost" id="loadConfigBtn" type="button" style="justify-self:start;">Load config</button>
            </div>
          </section>

          <section class="card">
            <div class="card-title">Filters</div>
            <div class="grid">
              <label>Preset
                <select id="preset">
                  <option value="">(none)</option>
                </select>
              </label>
              <p class="hint" id="presetHint">Load config to populate presets.</p>

              <label class="check">
                <input id="useAdhoc" type="checkbox" />
                Ad-hoc filters
              </label>

              <div id="adhocFields" class="grid">
                <div class="row-2">
                  <label>Include globs
                    <textarea id="includeGlobs" placeholder="**/*.pdf"></textarea>
                  </label>
                  <label>Exclude globs
                    <textarea id="excludeGlobs" placeholder="**/*draft*"></textarea>
                  </label>
                </div>
                <div class="row-2">
                  <label>Extensions
                    <input id="extensions" placeholder=".pdf, .md" />
                  </label>
                  <label>Path prefixes
                    <input id="prefixes" placeholder="rules/core" />
                  </label>
                </div>
              </div>
            </div>
          </section>

          <section class="card">
            <div class="card-title">Share</div>
            <label>Download URL
              <div class="share-row">
                <input id="shareUrl" readonly />
                <button class="ghost" id="copyUrlBtn" type="button">Copy</button>
              </div>
            </label>
          </section>
        </div>

        <div class="dl-main">
          <section class="card">
            <div class="preview-header">
              <div class="btn-row">
                <button class="ghost" id="previewBtn" type="button">Preview</button>
                <button class="btn-download" id="downloadBtn" type="button" disabled>Download ZIP</button>
              </div>
              <p id="message" class="message">Ready.</p>
            </div>

            <div class="stats-bar">
              <div class="stat-item">
                <span class="stat-label">Commit</span>
                <span class="stat-value" id="commitValue">&mdash;</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">Files</span>
                <span class="stat-value" id="filesValue">0</span>
              </div>
              <div class="stat-item">
                <span class="stat-label">Size</span>
                <span class="stat-value" id="bytesValue">0 B</span>
              </div>
            </div>

            <div id="treeView" class="tree"><span class="tree-empty">Run a preview to see matched files.</span></div>
            <p id="treeHint" class="small" style="margin-top:4px;"></p>
          </section>
        </div>
      </div>
    </div>

    <!-- ==================== CONFIGURE TAB ==================== -->
    <div class="tab-panel" id="panel-configure" role="tabpanel">
      <div class="config-layout">
        <section class="card">
          <div class="card-title">Repository Config</div>
          <p class="hint" style="margin-bottom:12px;">Edit <code>.zip-forger.yaml</code> presets and options, then save to the selected branch.</p>
          <div class="grid">
            <label class="check">
              <input id="allowAdhocFilters" type="checkbox" />
              Allow ad-hoc filters
            </label>
            <div class="row-2">
              <label>Max files per download
                <input id="maxFilesPerDownload" type="number" min="0" step="1" placeholder="0 = unlimited" />
              </label>
              <label>Max bytes per download
                <input id="maxBytesPerDownload" type="number" min="0" step="1" placeholder="0 = unlimited" />
              </label>
            </div>

            <div class="btn-row" style="margin-top:4px;">
              <button class="ghost" id="addPresetBtn" type="button">Add preset</button>
              <button class="primary" id="saveConfigBtn" type="button">Save config</button>
            </div>

            <div id="presetList" class="preset-list"></div>
          </div>
        </section>
      </div>
    </div>
  </main>

  <script>
    (function () {
      const AUTH_ENABLED = {{if .AuthEnabled}}true{{else}}false{{end}};
      const AUTH_REQUIRED = {{if .AuthRequired}}true{{else}}false{{end}};
      const THEME_KEY = "zip_forger.theme_mode";

      const state = {
        configLoaded: false,
        config: null,
        preview: null,
        busy: false
      };

      const nodes = {
        owner: document.getElementById("owner"),
        repo: document.getElementById("repo"),
        ref: document.getElementById("ref"),
        ownerOptions: document.getElementById("ownerOptions"),
        repoOptions: document.getElementById("repoOptions"),
        branchOptions: document.getElementById("branchOptions"),
        preset: document.getElementById("preset"),
        presetHint: document.getElementById("presetHint"),
        useAdhoc: document.getElementById("useAdhoc"),
        adhocFields: document.getElementById("adhocFields"),
        includeGlobs: document.getElementById("includeGlobs"),
        excludeGlobs: document.getElementById("excludeGlobs"),
        extensions: document.getElementById("extensions"),
        prefixes: document.getElementById("prefixes"),
        loadConfigBtn: document.getElementById("loadConfigBtn"),
        previewBtn: document.getElementById("previewBtn"),
        downloadBtn: document.getElementById("downloadBtn"),
        copyUrlBtn: document.getElementById("copyUrlBtn"),
        shareUrl: document.getElementById("shareUrl"),
        message: document.getElementById("message"),
        commitValue: document.getElementById("commitValue"),
        filesValue: document.getElementById("filesValue"),
        bytesValue: document.getElementById("bytesValue"),
        treeView: document.getElementById("treeView"),
        treeHint: document.getElementById("treeHint"),
        authBadge: document.getElementById("authBadge"),
        authBanner: document.getElementById("authBanner"),
        bannerLoginBtn: document.getElementById("bannerLoginBtn"),
        loginBtn: document.getElementById("loginBtn"),
        logoutBtn: document.getElementById("logoutBtn"),
        themeButtons: Array.from(document.querySelectorAll("[data-theme]")),
        tabButtons: Array.from(document.querySelectorAll("[data-tab]")),
        allowAdhocFilters: document.getElementById("allowAdhocFilters"),
        maxFilesPerDownload: document.getElementById("maxFilesPerDownload"),
        maxBytesPerDownload: document.getElementById("maxBytesPerDownload"),
        addPresetBtn: document.getElementById("addPresetBtn"),
        saveConfigBtn: document.getElementById("saveConfigBtn"),
        presetList: document.getElementById("presetList")
      };

      nodes.useAdhoc.checked = true;
      initTheme();
      wireEvents();
      hydrateAuth().then(() => initData());
      updateShareURL();

      /* ---- Loading state helpers ---- */
      function withLoading(btn, label, fn) {
        return async function () {
          if (state.busy) return;
          state.busy = true;
          const original = btn.textContent;
          btn.disabled = true;
          btn.textContent = label;
          try {
            await fn();
          } finally {
            btn.disabled = false;
            btn.textContent = original;
            state.busy = false;
            updateDownloadState();
          }
        };
      }

      function updateDownloadState() {
        nodes.downloadBtn.disabled = !state.preview || state.busy;
      }

      /* ---- Events ---- */
      function wireEvents() {
        nodes.loadConfigBtn.addEventListener("click", () => run(withLoading(nodes.loadConfigBtn, "Loading...", loadConfig)));
        nodes.previewBtn.addEventListener("click", () => run(withLoading(nodes.previewBtn, "Loading...", previewSelection)));
        nodes.downloadBtn.addEventListener("click", triggerDownload);
        nodes.copyUrlBtn.addEventListener("click", copyShareURL);
        nodes.logoutBtn.addEventListener("click", logout);
        nodes.addPresetBtn.addEventListener("click", () => addPresetRow());
        nodes.saveConfigBtn.addEventListener("click", () => run(withLoading(nodes.saveConfigBtn, "Saving...", saveConfig)));

        nodes.owner.addEventListener("change", () => run(onOwnerChanged));
        nodes.repo.addEventListener("change", () => run(onRepoChanged));
        nodes.ref.addEventListener("change", updateShareURL);
        nodes.preset.addEventListener("change", updateShareURL);
        nodes.useAdhoc.addEventListener("change", () => { updateAdhocVisibility(); updateShareURL(); });
        nodes.includeGlobs.addEventListener("input", updateShareURL);
        nodes.excludeGlobs.addEventListener("input", updateShareURL);
        nodes.extensions.addEventListener("input", updateShareURL);
        nodes.prefixes.addEventListener("input", updateShareURL);

        nodes.themeButtons.forEach((button) => {
          button.addEventListener("click", () => setThemeMode(button.dataset.theme));
        });

        nodes.tabButtons.forEach((button) => {
          button.addEventListener("click", () => switchTab(button.dataset.tab));
        });

        updateAdhocVisibility();
      }

      function updateAdhocVisibility() {
        nodes.adhocFields.style.display = nodes.useAdhoc.checked ? "grid" : "none";
      }

      function switchTab(tabId) {
        nodes.tabButtons.forEach((btn) => {
          const isActive = btn.dataset.tab === tabId;
          btn.classList.toggle("active", isActive);
          btn.setAttribute("aria-selected", String(isActive));
        });
        document.querySelectorAll(".tab-panel").forEach((panel) => {
          panel.classList.toggle("active", panel.id === "panel-" + tabId);
        });
      }

      async function run(fn) {
        try {
          await fn();
        } catch (err) {
          if (err && err.message) {
            setMessage(err.message, "err");
          }
        }
      }

      /* ---- Theme ---- */
      function initTheme() {
        const media = window.matchMedia("(prefers-color-scheme: dark)");
        const current = localStorage.getItem(THEME_KEY) || "system";
        applyTheme(current);
        media.addEventListener("change", () => {
          if ((localStorage.getItem(THEME_KEY) || "system") === "system") {
            applyTheme("system");
          }
        });
      }

      function setThemeMode(mode) {
        localStorage.setItem(THEME_KEY, mode);
        applyTheme(mode);
      }

      function applyTheme(mode) {
        const resolved = mode === "system"
          ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
          : mode;
        document.documentElement.dataset.theme = resolved;
        nodes.themeButtons.forEach((button) => {
          button.classList.toggle("active", button.dataset.theme === mode);
        });
      }

      /* ---- Auth ---- */
      async function hydrateAuth() {
        if (!AUTH_ENABLED) {
          nodes.authBadge.textContent = AUTH_REQUIRED ? "auth required" : "no auth";
          if (AUTH_REQUIRED) {
            nodes.authBanner.classList.add("visible");
          }
          return;
        }

        const returnTo = encodeURIComponent(window.location.pathname + window.location.search);
        nodes.loginBtn.href = "/auth/login?return_to=" + returnTo;
        nodes.bannerLoginBtn.href = "/auth/login?return_to=" + returnTo;
        nodes.loginBtn.hidden = false;

        try {
          const payload = await apiFetch("/auth/me", { credentials: "same-origin" });
          if (payload.authenticated) {
            nodes.authBadge.textContent = "signed in";
            nodes.logoutBtn.hidden = false;
            nodes.loginBtn.hidden = true;
          } else {
            nodes.authBadge.textContent = AUTH_REQUIRED ? "sign in required" : "not signed in";
            if (AUTH_REQUIRED) {
              nodes.authBanner.classList.add("visible");
            }
          }
        } catch (_err) {
          nodes.authBadge.textContent = "auth error";
          if (AUTH_REQUIRED) {
            nodes.authBanner.classList.add("visible");
          }
        }
      }

      async function logout() {
        await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
        window.location.reload();
      }

      /* ---- Data loading ---- */
      async function initData() {
        try {
          await loadOwners();
        } catch (_e) {}
        try {
          await onOwnerChanged();
        } catch (_e) {}
        try {
          await onRepoChanged();
        } catch (_e) {}
        try {
          await loadConfig();
        } catch (_e) {}
      }

      function currentSelection() {
        const owner = nodes.owner.value.trim();
        const repo = nodes.repo.value.trim();
        const ref = nodes.ref.value.trim();
        if (!owner || !repo) {
          return null;
        }
        return { owner, repo, ref };
      }

      async function loadOwners() {
        const payload = await apiFetch("/api/owners", { credentials: "same-origin" });
        setDatalist(nodes.ownerOptions, payload.owners || []);
      }

      async function onOwnerChanged() {
        updateShareURL();
        const owner = nodes.owner.value.trim();
        if (!owner) {
          setDatalist(nodes.repoOptions, []);
          return;
        }
        const payload = await apiFetch("/api/owners/" + encodeURIComponent(owner) + "/repos", { credentials: "same-origin" });
        setDatalist(nodes.repoOptions, payload.repos || []);
      }

      async function onRepoChanged() {
        updateShareURL();
        const selected = currentSelection();
        if (!selected) {
          setDatalist(nodes.branchOptions, []);
          return;
        }
        const payload = await apiFetch("/api/repos/" + encodeURIComponent(selected.owner) + "/" + encodeURIComponent(selected.repo) + "/branches", { credentials: "same-origin" });
        setDatalist(nodes.branchOptions, payload.branches || []);
      }

      async function loadConfig() {
        const selected = currentSelection();
        if (!selected) {
          throw new Error("Owner and repository are required.");
        }

        const query = new URLSearchParams();
        if (selected.ref) {
          query.set("ref", selected.ref);
        }
        const endpoint = "/api/repos/" + encodeURIComponent(selected.owner) + "/" + encodeURIComponent(selected.repo) + "/config?" + query.toString();
        const payload = await apiFetch(endpoint, { credentials: "same-origin" });

        state.configLoaded = true;
        state.config = payload.config || { version: 1, options: { allowAdhocFilters: true }, presets: [] };
        if (!state.config.options) {
          state.config.options = { allowAdhocFilters: true };
        }
        if (!Array.isArray(state.config.presets)) {
          state.config.presets = [];
        }

        nodes.commitValue.textContent = payload.commit || "\u2014";
        nodes.allowAdhocFilters.checked = state.config.options.allowAdhocFilters !== false;
        nodes.maxFilesPerDownload.value = formatLimitValue(state.config.options.maxFilesPerDownload);
        nodes.maxBytesPerDownload.value = formatLimitValue(state.config.options.maxBytesPerDownload);
        nodes.useAdhoc.disabled = state.config.options.allowAdhocFilters === false;
        if (nodes.useAdhoc.disabled) {
          nodes.useAdhoc.checked = false;
          updateAdhocVisibility();
        }

        renderPresetSelect();
        renderPresetEditor();
        nodes.presetHint.textContent = state.config.presets.length > 0
          ? state.config.presets.length + " preset(s) loaded."
          : "No presets in config.";
        setMessage("Config loaded.", "ok");
      }

      function renderPresetSelect() {
        const selected = nodes.preset.value;
        nodes.preset.innerHTML = "";
        nodes.preset.appendChild(optionNode("", "(none)"));
        for (const preset of state.config.presets) {
          const label = preset.description ? preset.id + " \u2014 " + preset.description : preset.id;
          nodes.preset.appendChild(optionNode(preset.id, label));
        }
        if (selected && state.config.presets.some((p) => p.id === selected)) {
          nodes.preset.value = selected;
        }
        updateShareURL();
      }

      function renderPresetEditor() {
        nodes.presetList.innerHTML = "";
        if (!state.config || !state.config.presets.length) {
          const empty = document.createElement("p");
          empty.className = "small";
          empty.textContent = "No presets yet.";
          nodes.presetList.appendChild(empty);
          return;
        }

        state.config.presets.forEach((preset, index) => {
          nodes.presetList.appendChild(buildPresetRow(preset, index));
        });
      }

      function buildPresetRow(preset, index) {
        const wrapper = document.createElement("section");
        wrapper.className = "preset";
        wrapper.dataset.index = String(index);

        const head = document.createElement("div");
        head.className = "preset-head";
        const title = document.createElement("strong");
        title.textContent = preset.id || "new-preset";
        const deleteButton = document.createElement("button");
        deleteButton.className = "danger-btn";
        deleteButton.type = "button";
        deleteButton.textContent = "Delete";
        deleteButton.addEventListener("click", () => {
          state.config.presets.splice(index, 1);
          renderPresetEditor();
          renderPresetSelect();
        });
        head.appendChild(title);
        head.appendChild(deleteButton);
        wrapper.appendChild(head);

        wrapper.appendChild(buildInputField("Preset ID", preset.id || "", (value) => {
          state.config.presets[index].id = value.trim();
          title.textContent = state.config.presets[index].id || "new-preset";
          renderPresetSelect();
        }));
        wrapper.appendChild(buildInputField("Description", preset.description || "", (value) => {
          state.config.presets[index].description = value.trim();
          renderPresetSelect();
        }));
        wrapper.appendChild(buildTextAreaField("Include globs", joinList(preset.includeGlobs), (value) => {
          state.config.presets[index].includeGlobs = splitList(value);
        }));
        wrapper.appendChild(buildTextAreaField("Exclude globs", joinList(preset.excludeGlobs), (value) => {
          state.config.presets[index].excludeGlobs = splitList(value);
        }));
        wrapper.appendChild(buildInputField("Extensions", joinList(preset.extensions), (value) => {
          state.config.presets[index].extensions = splitList(value);
        }));
        wrapper.appendChild(buildInputField("Path prefixes", joinList(preset.pathPrefixes), (value) => {
          state.config.presets[index].pathPrefixes = splitList(value);
        }));

        return wrapper;
      }

      function buildInputField(labelText, value, onChange) {
        const label = document.createElement("label");
        label.textContent = labelText;
        const input = document.createElement("input");
        input.value = value || "";
        input.addEventListener("input", () => onChange(input.value));
        label.appendChild(input);
        return label;
      }

      function buildTextAreaField(labelText, value, onChange) {
        const label = document.createElement("label");
        label.textContent = labelText;
        const textarea = document.createElement("textarea");
        textarea.value = value || "";
        textarea.addEventListener("input", () => onChange(textarea.value));
        label.appendChild(textarea);
        return label;
      }

      function addPresetRow() {
        if (!state.config) {
          state.config = { version: 1, options: { allowAdhocFilters: true }, presets: [] };
        }
        state.config.presets.push({
          id: "",
          description: "",
          includeGlobs: [],
          excludeGlobs: [],
          extensions: [],
          pathPrefixes: []
        });
        renderPresetEditor();
      }

      async function saveConfig() {
        const selected = currentSelection();
        if (!selected) {
          throw new Error("Owner and repository are required.");
        }
        if (!state.config) {
          throw new Error("No configuration loaded. Load config from the Download tab first.");
        }

        state.config.version = 1;
        if (!state.config.options) {
          state.config.options = {};
        }
        const maxSafeInteger = Number.MAX_SAFE_INTEGER;
        state.config.options.allowAdhocFilters = nodes.allowAdhocFilters.checked;
        state.config.options.maxFilesPerDownload = parseLimitInput(nodes.maxFilesPerDownload.value, "Max files per download", maxSafeInteger);
        state.config.options.maxBytesPerDownload = parseLimitInput(nodes.maxBytesPerDownload.value, "Max bytes per download", maxSafeInteger);
        state.config.presets = state.config.presets || [];

        const seen = new Set();
        for (const preset of state.config.presets) {
          const id = (preset.id || "").trim();
          if (!id) {
            throw new Error("Preset IDs must not be empty.");
          }
          if (seen.has(id)) {
            throw new Error("Preset IDs must be unique.");
          }
          seen.add(id);
          preset.id = id;
          preset.description = (preset.description || "").trim();
          preset.includeGlobs = splitList(joinList(preset.includeGlobs));
          preset.excludeGlobs = splitList(joinList(preset.excludeGlobs));
          preset.extensions = splitList(joinList(preset.extensions));
          preset.pathPrefixes = splitList(joinList(preset.pathPrefixes));
        }

        const endpoint = "/api/repos/" + encodeURIComponent(selected.owner) + "/" + encodeURIComponent(selected.repo) + "/config";
        await apiFetch(endpoint, {
          method: "PUT",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            ref: selected.ref || "main",
            config: state.config,
            commitMessage: "chore(zip-forger): update .zip-forger.yaml"
          })
        });

        setMessage("Config saved.", "ok");
        await loadConfig();
      }

      /* ---- Preview & Download ---- */
      async function previewSelection() {
        const selected = currentSelection();
        if (!selected) {
          throw new Error("Owner and repository are required.");
        }

        const body = {
          ref: selected.ref,
          preset: nodes.preset.value || ""
        };
        if (nodes.useAdhoc.checked) {
          body.adhoc = readAdhoc();
        }

        const endpoint = "/api/repos/" + encodeURIComponent(selected.owner) + "/" + encodeURIComponent(selected.repo) + "/preview";
        const payload = await apiFetch(endpoint, {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body)
        });

        state.preview = payload;
        nodes.commitValue.textContent = payload.commit || "\u2014";
        nodes.filesValue.textContent = String(payload.selectedFiles || 0);
        nodes.bytesValue.textContent = formatBytes(payload.totalBytes || 0);
        renderTree(payload.entries || [], !!payload.entriesTruncated);
        setMessage("Preview ready" + (payload.fromCache ? " (cached)." : "."), "ok");
      }

      function renderTree(paths, truncated) {
        if (!paths.length) {
          nodes.treeView.innerHTML = '<span class="tree-empty">No files matched your filters. Try adjusting your preset or ad-hoc filter settings.</span>';
          nodes.treeHint.textContent = "";
          return;
        }
        const lines = compactTreeLines(paths);
        nodes.treeView.textContent = lines.join("\n");
        nodes.treeHint.textContent = truncated
          ? "List truncated to first 2,000 entries."
          : "";
      }

      function compactTreeLines(paths) {
        const sorted = paths.slice().sort();
        const lines = [];
        let previous = [];
        for (const filePath of sorted) {
          const parts = filePath.split("/");
          let common = 0;
          while (common < previous.length && common < parts.length && previous[common] === parts[common]) {
            common += 1;
          }
          for (let i = common; i < parts.length; i++) {
            const isDir = i < parts.length - 1;
            lines.push("  ".repeat(i) + parts[i] + (isDir ? "/" : ""));
          }
          previous = parts;
        }
        return lines;
      }

      function triggerDownload() {
        if (!state.preview) {
          setMessage("Run a preview first.", "err");
          return;
        }
        const selected = currentSelection();
        if (!selected) {
          setMessage("Owner and repository are required.", "err");
          return;
        }
        window.location.assign(buildDownloadPath(selected));
      }

      async function copyShareURL() {
        const value = nodes.shareUrl.value;
        if (!value) return;
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(value);
        } else {
          nodes.shareUrl.focus();
          nodes.shareUrl.select();
          document.execCommand("copy");
        }
        setMessage("URL copied.", "ok");
      }

      function updateShareURL() {
        const selected = currentSelection();
        if (!selected) {
          nodes.shareUrl.value = "";
          return;
        }
        nodes.shareUrl.value = window.location.origin + buildDownloadPath(selected);
      }

      function buildDownloadPath(selected) {
        const query = new URLSearchParams();
        if (selected.ref) {
          query.set("ref", selected.ref);
        }
        if (nodes.preset.value) {
          query.set("preset", nodes.preset.value);
        }
        if (nodes.useAdhoc.checked) {
          const adhoc = readAdhoc();
          appendEach(query, "include", adhoc.includeGlobs);
          appendEach(query, "exclude", adhoc.excludeGlobs);
          appendEach(query, "ext", adhoc.extensions);
          appendEach(query, "prefix", adhoc.pathPrefixes);
        }
        return "/api/repos/" + encodeURIComponent(selected.owner) + "/" + encodeURIComponent(selected.repo) + "/download.zip?" + query.toString();
      }

      function readAdhoc() {
        return {
          includeGlobs: splitList(nodes.includeGlobs.value),
          excludeGlobs: splitList(nodes.excludeGlobs.value),
          extensions: splitList(nodes.extensions.value),
          pathPrefixes: splitList(nodes.prefixes.value)
        };
      }

      /* ---- Utilities ---- */
      function setDatalist(listNode, values) {
        listNode.innerHTML = "";
        for (const value of values) {
          const option = document.createElement("option");
          option.value = value;
          listNode.appendChild(option);
        }
      }

      function optionNode(value, label) {
        const option = document.createElement("option");
        option.value = value;
        option.textContent = label;
        return option;
      }

      function splitList(value) {
        if (Array.isArray(value)) {
          return value.map((item) => String(item).trim()).filter(Boolean);
        }
        return String(value || "")
          .split(/[\n,]/g)
          .map((item) => item.trim())
          .filter(Boolean);
      }

      function joinList(value) {
        if (!value) return "";
        if (Array.isArray(value)) return value.join(", ");
        return String(value);
      }

      function appendEach(params, key, values) {
        for (const value of values) {
          params.append(key, value);
        }
      }

      function formatBytes(bytes) {
        const units = ["B", "KB", "MB", "GB", "TB"];
        let size = Number(bytes) || 0;
        let index = 0;
        while (size >= 1024 && index < units.length - 1) {
          size /= 1024;
          index += 1;
        }
        return size.toFixed(index === 0 ? 0 : size < 10 ? 2 : 1) + " " + units[index];
      }

      function formatLimitValue(value) {
        const numeric = Number(value);
        if (!Number.isFinite(numeric) || numeric <= 0) return "";
        return String(Math.floor(numeric));
      }

      function parseLimitInput(value, fieldName, maxValue) {
        const trimmed = String(value || "").trim();
        if (!trimmed) return 0;
        if (!/^[0-9]+$/.test(trimmed)) {
          throw new Error(fieldName + " must be a whole number >= 0.");
        }
        const parsed = Number(trimmed);
        if (!Number.isSafeInteger(parsed) || parsed < 0) {
          throw new Error(fieldName + " must be a whole number >= 0.");
        }
        if (parsed > maxValue) {
          throw new Error(fieldName + " is too large.");
        }
        return parsed;
      }

      function setMessage(text, level) {
        nodes.message.textContent = text;
        nodes.message.className = "message" + (level ? " " + level : "");
      }

      async function apiFetch(url, options) {
        const response = await fetch(url, options);
        const text = await response.text();
        let payload = {};
        if (text) {
          try {
            payload = JSON.parse(text);
          } catch (_err) {
            payload = {};
          }
        }
        if (!response.ok) {
          const message = payload && payload.error && payload.error.message
            ? payload.error.message
            : ("request failed with HTTP " + response.status);
          throw new Error(message);
        }
        return payload;
      }
    })();
  </script>
</body>
</html>`))

func RenderIndex(w http.ResponseWriter, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, data); err != nil {
		http.Error(w, "failed to render ui", http.StatusInternalServerError)
	}
}
