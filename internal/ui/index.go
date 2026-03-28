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
      --bg: #f4f4f6;
      --bg-accent: #eaeaee;
      --surface: rgba(255, 255, 255, 0.85);
      --surface-alt: rgba(255, 255, 255, 0.58);
      --text: #18181f;
      --text-secondary: #3a3a4a;
      --muted: #6b6b80;
      --line: rgba(24, 24, 40, 0.13);
      --line-strong: rgba(24, 24, 40, 0.24);
      --brand: #4361b8;
      --brand-strong: #2d4a9e;
      --brand-soft: rgba(67, 97, 184, 0.08);
      --warn: #9f6324;
      --danger: #9e2f3a;
      --danger-soft: rgba(158, 47, 58, 0.08);
      --shadow: 0 1px 3px rgba(20, 20, 40, 0.06), 0 8px 32px rgba(20, 20, 40, 0.1);
      --shadow-lg: 0 4px 12px rgba(20, 20, 40, 0.08), 0 20px 48px rgba(20, 20, 40, 0.13);
      --radius: 12px;
      --radius-sm: 8px;
    }

    :root[data-theme="dark"] {
      color-scheme: dark;
      --bg: #111118;
      --bg-accent: #1c1c26;
      --surface: rgba(30, 30, 44, 0.88);
      --surface-alt: rgba(22, 22, 34, 0.68);
      --text: #e6e6f0;
      --text-secondary: #b8b8cc;
      --muted: #7878a0;
      --line: rgba(200, 200, 230, 0.13);
      --line-strong: rgba(200, 200, 230, 0.24);
      --brand: #7b9cf4;
      --brand-strong: #99b4f8;
      --brand-soft: rgba(123, 156, 244, 0.1);
      --warn: #e2a765;
      --danger: #f48e9a;
      --danger-soft: rgba(244, 142, 154, 0.1);
      --shadow: 0 1px 3px rgba(0, 0, 0, 0.14), 0 8px 32px rgba(0, 0, 0, 0.3);
      --shadow-lg: 0 4px 12px rgba(0, 0, 0, 0.18), 0 20px 48px rgba(0, 0, 0, 0.38);
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

    .auth-badge {
      font-size: 0.78rem;
      font-weight: 500;
      color: var(--muted);
      padding: 4px 10px 4px 8px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--surface-alt);
      white-space: nowrap;
      display: inline-flex;
      align-items: center;
      gap: 6px;
    }

    .auth-badge::before {
      content: "";
      display: inline-block;
      width: 7px;
      height: 7px;
      border-radius: 50%;
      background: var(--muted);
      flex-shrink: 0;
    }

    .auth-badge.state-signed-in {
      color: #1a6e3c;
      border-color: rgba(26, 110, 60, 0.3);
      background: rgba(26, 110, 60, 0.07);
    }
    .auth-badge.state-signed-in::before { background: #2a9955; }

    :root[data-theme="dark"] .auth-badge.state-signed-in {
      color: #6ddb96;
      border-color: rgba(109, 219, 150, 0.25);
      background: rgba(109, 219, 150, 0.08);
    }
    :root[data-theme="dark"] .auth-badge.state-signed-in::before { background: #6ddb96; }

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

    .tab-btn:hover { color: var(--text-secondary); }

    .tab-btn.active {
      color: var(--brand-strong);
      border-bottom-color: var(--brand);
    }

    :root[data-theme="dark"] .tab-btn.active { color: var(--brand); }

    .tab-panel { display: none; }
    .tab-panel.active { display: block; }

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

    .dl-layout {
      display: grid;
      grid-template-columns: 380px 1fr;
      gap: 16px;
      align-items: start;
    }

    .dl-sidebar { display: grid; gap: 12px; position: sticky; top: 20px; }
    .dl-main { display: grid; gap: 12px; }
    .grid { display: grid; gap: 10px; }
    .row-2 { display: grid; gap: 10px; grid-template-columns: 1fr 1fr; }

    label { display: grid; gap: 4px; font-size: 0.82rem; font-weight: 500; color: var(--muted); }
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
    }

    :root[data-theme="dark"] input, :root[data-theme="dark"] select, :root[data-theme="dark"] textarea {
      background: rgba(12, 18, 16, 0.75);
    }

    button, .btn {
      border: 1px solid transparent;
      border-radius: var(--radius-sm);
      padding: 7px 14px;
      cursor: pointer;
      text-decoration: none;
      font-weight: 500;
      font-size: 0.86rem;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      min-height: 34px;
    }

    .primary { color: #fff; background: var(--brand); }
    .ghost { color: var(--brand-strong); background: var(--surface-alt); border-color: var(--line); }
    :root[data-theme="dark"] .ghost { color: var(--brand); }
    .danger-btn { color: #fff; background: var(--danger); font-size: 0.8rem; padding: 4px 10px; min-height: 28px; }
    .btn-download { color: #fff; background: var(--brand-strong); font-size: 0.92rem; padding: 9px 20px; min-height: 38px; font-weight: 600; }
    :root[data-theme="dark"] .btn-download { background: var(--brand); color: #111118; }

    .message { min-height: 1rem; color: var(--muted); font-size: 0.86rem; flex-grow: 1; }
    .message.ok { color: #1a6e3c; }
    :root[data-theme="dark"] .message.ok { color: #6ddb96; }
    .message.err { color: var(--danger); }

    .stats-bar { display: flex; gap: 16px; padding: 8px 0; font-size: 0.84rem; }
    .stat-label { color: var(--muted); text-transform: uppercase; font-size: 0.72rem; font-weight: 600; }
    .stat-value { color: var(--text-secondary); font-family: "JetBrains Mono", monospace; }

    .tree {
      border: 1px solid var(--line);
      border-radius: var(--radius-sm);
      background: var(--surface-alt);
      min-height: 300px;
      max-height: calc(100vh - 320px);
      overflow: auto;
      padding: 12px 14px;
      font-family: "JetBrains Mono", monospace;
      font-size: 0.78rem;
      white-space: pre;
      overflow-x: auto;
      overflow-wrap: break-word;
    }

    .repo-summary { font-family: "JetBrains Mono", monospace; font-size: 0.8rem; color: var(--brand-strong); background: var(--brand-soft); border: 1px solid rgba(67, 97, 184, 0.2); border-radius: var(--radius-sm); padding: 6px 10px; }
    :root[data-theme="dark"] .repo-summary { color: var(--brand); border-color: rgba(123, 156, 244, 0.2); }

    .index-bar-bg { height:4px; background:var(--line); border-radius:2px; overflow:hidden; margin-top:4px; }
    .index-bar-fill { height:100%; width:0; background:var(--brand); transition: width 0.3s; }

    .preset-item {
      border: 1px solid var(--line);
      border-radius: var(--radius-sm);
      padding: 12px;
      margin-bottom: 10px;
      display: grid;
      gap: 8px;
      background: var(--surface-alt);
    }

    .preset-item-head {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }

    @media (max-width: 840px) {
      .dl-layout { grid-template-columns: 1fr; }
      .dl-sidebar { position: static; }
    }
  </style>
</head>
<body>
  <main class="frame">
    <header class="header">
      <div class="header-left">
        <h1 class="title">zip-forger</h1>
        <span class="auth-badge" id="authBadge">checking...</span>
        <p id="message" class="message" style="margin-left:12px; font-weight:500;"></p>
        <a class="btn ghost" id="loginBtn" href="/auth/login" hidden>Sign in</a>
        <button class="ghost" id="logoutBtn" type="button" hidden>Sign out</button>
      </div>
      <div class="header-right">
        <div class="theme-toggle">
          <button class="theme-btn" type="button" data-theme="system">Sys</button>
          <button class="theme-btn" type="button" data-theme="light">Light</button>
          <button class="theme-btn" type="button" data-theme="dark">Dark</button>
        </div>
      </div>
    </header>

    <div class="auth-banner" id="authBanner">Authentication is required.</div>

    <nav class="tab-bar">
      <button class="tab-btn active" data-tab="download">Download</button>
      <button class="tab-btn" id="configureTabBtn" data-tab="configure" disabled>Configure</button>
    </nav>

    <div class="tab-panel active" id="panel-download">
      <div class="dl-layout">
        <div class="dl-sidebar">
          <section class="card">
            <div class="card-title">Repository</div>
            <div class="grid">
              <label>Repository
                <input id="repo" list="repoOptions" placeholder="owner/repo" autocomplete="off" />
                <datalist id="repoOptions"></datalist>
              </label>
              <label>Branch / Ref
                <input id="ref" list="branchOptions" value="main" autocomplete="off" />
                <datalist id="branchOptions"></datalist>
              </label>
              <div id="repoSummary" class="repo-summary" hidden></div>
              <button class="ghost" id="loadConfigBtn" type="button">Load config</button>
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
              <label class="check"><input id="useAdhoc" type="checkbox" /> Ad-hoc filters</label>
              <div id="adhocFields" class="grid">
                <textarea id="includeGlobs" placeholder="Include globs (one per line)"></textarea>
                <textarea id="excludeGlobs" placeholder="Exclude globs (one per line)"></textarea>
                <input id="extensions" placeholder="Extensions (e.g. .pdf, .md)" />
                <input id="prefixes" placeholder="Path prefixes (e.g. src/)" />
              </div>
            </div>
          </section>

          <section class="card">
            <div class="card-title">Download URL</div>
            <label class="check"><input id="privateDownloadUrl" type="checkbox" /> Private URL</label>
            <input id="shareUrl" readonly />
            <button class="ghost" id="copyUrlBtn" type="button">Copy URL</button>
          </section>
        </div>

        <div class="dl-main">
          <section class="card">
            <div class="grid">
              <div style="display:flex; gap:8px; align-items:center;">
                <button class="ghost" id="previewBtn" type="button">Preview</button>
                <button class="btn-download" id="downloadBtn" type="button" disabled>Download ZIP</button>
              </div>
              
              <div id="indexProgress" hidden>
                <div style="display:flex; justify-content:space-between; font-size:0.8rem; color:var(--muted);">
                  <span id="indexStatus">Indexing repository...</span>
                  <span id="indexCount">0 files discovered</span>
                </div>
                <div class="index-bar-bg"><div id="indexBar" class="index-bar-fill"></div></div>
              </div>

              <div class="stats-bar">
                <div class="stat-item"><span class="stat-label">Commit</span> <span class="stat-value" id="commitValue">&mdash;</span></div>
                <div class="stat-item"><span class="stat-label">Files</span> <span class="stat-value" id="filesValue">0</span></div>
                <div class="stat-item"><span class="stat-label">Size</span> <span class="stat-value" id="bytesValue">0 B</span></div>
              </div>
              <div id="treeView" class="tree"></div>
            </div>
          </section>
        </div>
      </div>
    </div>

    <div class="tab-panel" id="panel-configure">
      <div class="card" style="max-width:800px; margin:0 auto;">
        <div class="card-title">Repository Configuration</div>
        <div class="grid">
          <label class="check"><input id="allowAdhocFilters" type="checkbox" /> Allow ad-hoc filters</label>
          <button class="ghost" id="addPresetBtn" type="button" style="justify-self:start;">Add Preset</button>
          <div id="presetList" class="grid"></div>
          <button class="primary" id="saveConfigBtn" type="button">Save to Repository</button>
        </div>
      </div>
    </div>
  </main>

  <script>
    (function () {
      const THEME_KEY = "zip_forger.theme_mode";
      const authEnabled = {{if .AuthEnabled}}true{{else}}false{{end}};
      const authRequired = {{if .AuthRequired}}true{{else}}false{{end}};
      const state = { busy: false, preview: null, previewKey: "", previewPrivateDownload: false, config: null };
      const nodes = {
        repo: document.getElementById("repo"),
        repoOptions: document.getElementById("repoOptions"),
        ref: document.getElementById("ref"),
        branchOptions: document.getElementById("branchOptions"),
        repoSummary: document.getElementById("repoSummary"),
        preset: document.getElementById("preset"),
        useAdhoc: document.getElementById("useAdhoc"),
        adhocFields: document.getElementById("adhocFields"),
        includeGlobs: document.getElementById("includeGlobs"),
        excludeGlobs: document.getElementById("excludeGlobs"),
        extensions: document.getElementById("extensions"),
        prefixes: document.getElementById("prefixes"),
        privateDownloadUrl: document.getElementById("privateDownloadUrl"),
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
        indexProgress: document.getElementById("indexProgress"),
        indexStatus: document.getElementById("indexStatus"),
        indexCount: document.getElementById("indexCount"),
        indexBar: document.getElementById("indexBar"),
        authBadge: document.getElementById("authBadge"),
        authBanner: document.getElementById("authBanner"),
        loginBtn: document.getElementById("loginBtn"),
        logoutBtn: document.getElementById("logoutBtn"),
        themeButtons: Array.from(document.querySelectorAll("[data-theme]")),
        allowAdhocFilters: document.getElementById("allowAdhocFilters"),
        addPresetBtn: document.getElementById("addPresetBtn"),
        saveConfigBtn: document.getElementById("saveConfigBtn"),
        presetList: document.getElementById("presetList"),
        configureTabBtn: document.getElementById("configureTabBtn")
      };

      initTheme();
      wireEvents();
      hydrateAuth();
      hydrateFromUrl();

      function wireEvents() {
        nodes.previewBtn.addEventListener("click", () => run(withLoading(nodes.previewBtn, "Loading...", previewSelection)));
        nodes.loadConfigBtn.addEventListener("click", () => run(withLoading(nodes.loadConfigBtn, "Loading...", loadConfig)));
        nodes.saveConfigBtn.addEventListener("click", () => run(withLoading(nodes.saveConfigBtn, "Saving...", saveConfig)));
        nodes.downloadBtn.addEventListener("click", downloadZip);
        nodes.addPresetBtn.addEventListener("click", () => addPresetRow());
        nodes.logoutBtn.addEventListener("click", logout);
        nodes.useAdhoc.addEventListener("change", () => { updateAdhocVisibility(); invalidatePreview(); updateShareURL(); });
        nodes.privateDownloadUrl.addEventListener("change", updateShareURL);
        nodes.copyUrlBtn.addEventListener("click", copyShareURL);
        
        const updateEvents = ["input", "change"];
        [nodes.ref, nodes.preset, nodes.includeGlobs, nodes.excludeGlobs, nodes.extensions, nodes.prefixes].forEach(node => {
          updateEvents.forEach(ev => node.addEventListener(node === nodes.includeGlobs || node === nodes.excludeGlobs ? "input" : ev, () => {
            invalidatePreview();
            updateShareURL();
          }));
        });

        nodes.repo.addEventListener("input", debounce(() => {
          const q = nodes.repo.value;
          if (q.length < 2) return;
          fetch("/api/repos/search?q=" + encodeURIComponent(q)).then(r => r.json()).then(data => {
            nodes.repoOptions.innerHTML = (data.repos || []).map(r => '<option value="' + r + '">').join("");
          });
        }, 300));

        nodes.repo.addEventListener("change", () => run(async () => { 
          invalidatePreview();
          await onRepoChanged(); 
          if (nodes.repo.value.includes("/")) {
            await loadConfig(); 
          }
        }));
        nodes.ref.addEventListener("change", () => run(async () => {
          invalidatePreview();
          await onRepoChanged();
          await loadConfig();
        }));

        nodes.themeButtons.forEach(btn => {
          btn.addEventListener("click", () => setThemeMode(btn.dataset.theme));
        });

        document.querySelectorAll(".tab-btn").forEach(btn => {
          btn.addEventListener("click", () => {
            if (btn.disabled) return;
            document.querySelectorAll(".tab-btn, .tab-panel").forEach(el => el.classList.remove("active"));
            btn.classList.add("active");
            document.getElementById("panel-" + btn.dataset.tab).classList.add("active");
          });
        });

        updateAdhocVisibility();
      }

      function updateAdhocVisibility() {
        nodes.adhocFields.style.display = nodes.useAdhoc.checked ? "grid" : "none";
      }

      async function run(fn) {
        try { await fn(); } catch (err) { setMessage(err.message, "err"); }
      }

      function withLoading(btn, label, fn) {
        return async function () {
          if (state.busy) return;
          state.busy = true;
          const original = btn.textContent;
          btn.disabled = true;
          btn.textContent = label;
          try { await fn(); } finally {
            btn.disabled = false;
            btn.textContent = original;
            state.busy = false;
          }
        };
      }

      function setMessage(text, level) {
        nodes.message.textContent = text;
        nodes.message.className = "message" + (level ? " " + level : "");
      }

      function currentSelectionKey() {
        return JSON.stringify({
          repo: nodes.repo.value.trim(),
          ref: nodes.ref.value.trim(),
          preset: nodes.preset.value.trim(),
          adhoc: !!nodes.useAdhoc.checked,
          include: nodes.includeGlobs.value,
          exclude: nodes.excludeGlobs.value,
          ext: nodes.extensions.value,
          prefix: nodes.prefixes.value
        });
      }

      function invalidatePreview() {
        state.preview = null;
        state.previewKey = "";
        state.previewPrivateDownload = false;
        nodes.downloadBtn.disabled = true;
        nodes.commitValue.textContent = "—";
        nodes.filesValue.textContent = "0";
        nodes.bytesValue.textContent = "0 B";
        nodes.treeView.textContent = "";
        updateRepoSummary();
      }

      function buildCurrentDownloadURL() {
        const full = nodes.repo.value.trim();
        const parts = full.split("/");
        if (parts.length < 2) return "";

        const query = new URLSearchParams();
        if (nodes.ref.value) query.set("ref", nodes.ref.value);
        if (nodes.preset.value) query.set("preset", nodes.preset.value);
        if (nodes.useAdhoc.checked) {
          nodes.includeGlobs.value.split("\n").filter(Boolean).forEach(g => query.append("include", g));
          nodes.excludeGlobs.value.split("\n").filter(Boolean).forEach(g => query.append("exclude", g));
          nodes.extensions.value.split(",").map(s => s.trim()).filter(Boolean).forEach(e => query.append("ext", e));
          nodes.prefixes.value.split(",").map(s => s.trim()).filter(Boolean).forEach(p => query.append("prefix", p));
        }

        const base = "/api/repos/" + encodeURIComponent(parts[0]) + "/" + encodeURIComponent(parts[1]) + "/download.zip";
        const qs = query.toString();
        return qs ? base + "?" + qs : base;
      }

      function buildCurrentAdhocPayload() {
        if (!nodes.useAdhoc.checked) {
          return {
            includeGlobs: [],
            excludeGlobs: [],
            extensions: [],
            pathPrefixes: []
          };
        }

        return {
          includeGlobs: nodes.includeGlobs.value.split("\n").filter(Boolean),
          excludeGlobs: nodes.excludeGlobs.value.split("\n").filter(Boolean),
          extensions: nodes.extensions.value.split(",").map(s => s.trim()).filter(Boolean),
          pathPrefixes: nodes.prefixes.value.split(",").map(s => s.trim()).filter(Boolean)
        };
      }

      function updateShareURL() {
        if (!nodes.repo.value.includes("/")) {
          nodes.shareUrl.value = "";
          nodes.configureTabBtn.disabled = true;
          return;
        }
        nodes.configureTabBtn.disabled = false;

        const previewMatches = state.preview
          && state.previewKey === currentSelectionKey()
          && state.previewPrivateDownload === nodes.privateDownloadUrl.checked;
        let url = "";
        if (previewMatches && state.preview.downloadUrl) {
          url = state.preview.downloadUrl;
        } else if (!nodes.privateDownloadUrl.checked) {
          url = buildCurrentDownloadURL();
        }
        nodes.shareUrl.value = url ? new URL(url, window.location.origin).toString() : "";
        updateRepoSummary();
      }

      function copyShareURL() {
        if (!nodes.shareUrl.value) {
          setMessage(nodes.privateDownloadUrl.checked ? "Run preview to generate a private URL." : "No download URL available.", "err");
          return;
        }
        nodes.shareUrl.select();
        document.execCommand("copy");
        setMessage("URL copied to clipboard.", "ok");
      }

      function hydrateFromUrl() {
        const params = new URL(window.location.href).searchParams;
        if (params.get("repo")) nodes.repo.value = params.get("repo");
        if (params.get("ref")) nodes.ref.value = params.get("ref");
        if (params.get("preset")) nodes.preset.value = params.get("preset");
        if (params.get("adhoc") === "1") {
          nodes.useAdhoc.checked = true;
          nodes.includeGlobs.value = params.getAll("include").join("\n");
          nodes.excludeGlobs.value = params.getAll("exclude").join("\n");
          nodes.extensions.value = params.get("ext") || "";
          nodes.prefixes.value = params.get("prefix") || "";
        }
        if (nodes.repo.value) {
          onRepoChanged();
          loadConfig();
        }
        updateAdhocVisibility();
        updateShareURL();
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
        nodes.themeButtons.forEach(btn => {
          btn.classList.toggle("active", btn.dataset.theme === mode);
        });
      }

      let progressSource = null;
      function startProgressTracking(owner, repo, ref) {
        stopProgressTracking();
        const url = "/api/repos/" + encodeURIComponent(owner) + "/" + encodeURIComponent(repo) + "/index-progress?ref=" + encodeURIComponent(ref);
        progressSource = new EventSource(url);
        nodes.indexProgress.hidden = false;
        nodes.indexStatus.textContent = "Indexing repository...";
        nodes.indexCount.textContent = "0 files discovered";
        nodes.indexBar.style.width = "0%";
        progressSource.onmessage = (e) => {
          const data = JSON.parse(e.data);
          nodes.indexCount.textContent = data.count.toLocaleString() + " files discovered";
          if (data.phase === "finalizing") {
            nodes.indexStatus.textContent = "Finalizing selection...";
            nodes.indexBar.style.width = "100%";
            return;
          }
          nodes.indexStatus.textContent = "Indexing repository...";
          const val = (Math.log10(data.count + 1) / 6) * 100;
          nodes.indexBar.style.width = Math.min(val, 99) + "%";
        };
        progressSource.onerror = () => {
          stopProgressTracking();
        };
      }

      function stopProgressTracking() {
        if (progressSource) { progressSource.close(); progressSource = null; }
        nodes.indexProgress.hidden = true;
      }

      async function apiFetch(url, opts = {}) {
        const res = await fetch(url, opts);
        if (res.status === 204) return {};
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          if (authEnabled && authRequired && res.status === 401) {
            nodes.authBanner.classList.add("visible");
          }
          const msg = (data.error && data.error.message) || data.message || "Request failed: " + res.status;
          throw new Error(msg);
        }
        nodes.authBanner.classList.remove("visible");
        return data;
      }

      function updateRepoSummary() {
        const full = nodes.repo.value.trim();
        const parts = full.split("/");
        if (parts.length < 2) { nodes.repoSummary.hidden = true; return; }
        let text = parts[0] + " / " + parts[1] + (nodes.ref.value ? " @ " + nodes.ref.value : "");
        if (state.preview) {
          text += " (" + state.preview.selectedFiles.toLocaleString() + " files, " + formatBytes(state.preview.totalBytes) + ")";
        }
        nodes.repoSummary.textContent = text;
        nodes.repoSummary.hidden = false;
      }

      async function onRepoChanged() {
        const parts = nodes.repo.value.split("/");
        if (parts.length < 2) return;
        const data = await apiFetch("/api/repos/" + encodeURIComponent(parts[0]) + "/" + encodeURIComponent(parts[1]) + "/branches");
        nodes.branchOptions.innerHTML = (data.branches || []).map(b => '<option value="' + b + '">').join("");
        updateShareURL();
      }

      async function downloadZip() {
        const url = buildCurrentDownloadURL();
        if (!url) return;
        // Crucial: Use a temporary link to ensure browsers handle the download 
        // while preserving the user session (cookies).
        const a = document.createElement("a");
        a.href = url;
        a.download = "";
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
      }

      async function previewSelection() {
        const full = nodes.repo.value.trim();
        const parts = full.split("/");
        if (parts.length < 2) return;
        
        try {
          startProgressTracking(parts[0], parts[1], nodes.ref.value || "main");
          const payload = await apiFetch("/api/repos/" + encodeURIComponent(parts[0]) + "/" + encodeURIComponent(parts[1]) + "/preview", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              ref: nodes.ref.value,
              preset: nodes.preset.value,
              adhoc: buildCurrentAdhocPayload(),
              privateDownloadUrl: nodes.privateDownloadUrl.checked
            })
          });
          state.preview = payload;
          state.previewKey = currentSelectionKey();
          state.previewPrivateDownload = nodes.privateDownloadUrl.checked;
          nodes.commitValue.textContent = payload.commit.substring(0,8);
          nodes.filesValue.textContent = payload.selectedFiles.toLocaleString();
          nodes.bytesValue.textContent = formatBytes(payload.totalBytes);
          nodes.treeView.textContent = (payload.entries || []).join("\n");
          nodes.downloadBtn.disabled = false;
          setMessage("Preview ready.", "ok");
          updateShareURL();
        } finally {
          stopProgressTracking();
        }
      }

      async function loadConfig() {
        const full = nodes.repo.value.trim();
        const parts = full.split("/");
        if (parts.length < 2) return;
        const query = nodes.ref.value ? "?ref=" + encodeURIComponent(nodes.ref.value) : "";
        const data = await apiFetch("/api/repos/" + encodeURIComponent(parts[0]) + "/" + encodeURIComponent(parts[1]) + "/config" + query);
        state.config = data.config || { version: 1, options: { allowAdhocFilters: true }, presets: [] };
        
        updatePresetDropdown();
        nodes.allowAdhocFilters.checked = state.config.options?.allowAdhocFilters !== false;
        renderPresetEditor();
        setMessage("Config loaded.", "ok");
      }

      function updatePresetDropdown() {
        nodes.preset.innerHTML = '<option value="">(none)</option>';
        (state.config.presets || []).forEach(p => {
          const opt = document.createElement("option");
          opt.value = p.id;
          opt.textContent = p.id + (p.description ? " - " + p.description : "");
          nodes.preset.appendChild(opt);
        });
      }

      function renderPresetEditor() {
        nodes.presetList.innerHTML = "";
        (state.config.presets || []).forEach((p, i) => {
          const item = document.createElement("div");
          item.className = "preset-item";
          item.innerHTML = 
            '<div class="preset-item-head">' +
              '<strong>' + (p.id || "new-preset") + '</strong>' +
              '<button class="danger-btn" onclick="removePreset(' + i + ')">Delete</button>' +
            '</div>' +
            '<label>ID <input value="' + (p.id || "") + '" oninput="updatePreset(' + i + ', \'id\', this.value)"></label>' +
            '<label>Description <input value="' + (p.description || "") + '" oninput="updatePreset(' + i + ', \'description\', this.value)"></label>' +
            '<label>Include Globs <input value="' + (p.includeGlobs || []).join(", ") + '" oninput="updatePreset(' + i + ', \'includeGlobs\', this.value)"></label>' +
            '<label>Exclude Globs <input value="' + (p.excludeGlobs || []).join(", ") + '" oninput="updatePreset(' + i + ', \'excludeGlobs\', this.value)"></label>' +
            '<label>Extensions <input value="' + (p.extensions || []).join(", ") + '" oninput="updatePreset(' + i + ', \'extensions\', this.value)"></label>' +
            '<label>Prefixes <input value="' + (p.pathPrefixes || []).join(", ") + '" oninput="updatePreset(' + i + ', \'pathPrefixes\', this.value)"></label>';
          nodes.presetList.appendChild(item);
        });
      }

      window.updatePreset = (idx, key, val) => {
        if (key === 'extensions' || key === 'pathPrefixes' || key === 'includeGlobs' || key === 'excludeGlobs') {
          state.config.presets[idx][key] = val.split(",").map(s => s.trim()).filter(Boolean);
        } else {
          state.config.presets[idx][key] = val;
        }
        updatePresetDropdown();
      };

      window.removePreset = (idx) => {
        state.config.presets.splice(idx, 1);
        renderPresetEditor();
        updatePresetDropdown();
      };

      function addPresetRow() {
        if (!state.config) {
          state.config = { version: 1, options: { allowAdhocFilters: true }, presets: [] };
        }
        state.config.presets = state.config.presets || [];
        state.config.presets.push({ id: "", description: "", includeGlobs: [], excludeGlobs: [], extensions: [], pathPrefixes: [] });
        renderPresetEditor();
      }

      async function saveConfig() {
        const full = nodes.repo.value.trim();
        const parts = full.split("/");
        if (parts.length < 2) return;
        state.config.options = state.config.options || {};
        state.config.options.allowAdhocFilters = nodes.allowAdhocFilters.checked;
        try {
          await apiFetch("/api/repos/" + encodeURIComponent(parts[0]) + "/" + encodeURIComponent(parts[1]) + "/config", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              ref: nodes.ref.value || "main",
              config: state.config,
              commitMessage: "chore: update presets"
            })
          });
          setMessage("Config saved successfully.", "ok");
        } catch (e) {
          setMessage("Save failed: " + e.message + ". Try signing out and in again to refresh permissions.", "err");
          throw e;
        }
      }

      function formatBytes(b) {
        if (b < 1024) return b + " B";
        const units = ["KB", "MB", "GB"];
        let i = -1;
        do { b /= 1024; i++; } while (b > 1024 && i < units.length - 1);
        return b.toFixed(2) + " " + units[i];
      }

      function debounce(fn, ms) {
        let t;
        return function(...args) { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
      }

      async function logout() {
        await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
        window.location.reload();
      }

      async function hydrateAuth() {
        if (!authEnabled) {
          nodes.authBadge.textContent = "auth disabled";
          nodes.loginBtn.hidden = true;
          nodes.logoutBtn.hidden = true;
          nodes.authBanner.classList.remove("visible");
          return;
        }

        const returnTo = encodeURIComponent(window.location.pathname + window.location.search);
        nodes.loginBtn.href = "/auth/login?return_to=" + returnTo;

        const data = await apiFetch("/auth/me").catch(() => ({}));
        if (data.authenticated) {
          nodes.authBadge.textContent = "signed in";
          nodes.authBadge.classList.add("state-signed-in");
          nodes.logoutBtn.hidden = false;
        } else {
          nodes.authBadge.textContent = "not signed in";
          nodes.loginBtn.hidden = false;
        }
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
