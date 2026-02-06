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
  <style>
    :root {
      color-scheme: light;
      --bg: #faf5e8;
      --bg-accent: #ece3cf;
      --surface: rgba(255, 255, 255, 0.86);
      --surface-alt: rgba(255, 255, 255, 0.68);
      --text: #16231f;
      --muted: #54635d;
      --line: rgba(22, 35, 31, 0.2);
      --brand: #2f7352;
      --brand-strong: #1e4f39;
      --warn: #9f6324;
      --danger: #9e2f3a;
      --shadow: 0 16px 44px rgba(30, 45, 38, 0.14);
      --radius: 14px;
    }

    :root[data-theme="dark"] {
      color-scheme: dark;
      --bg: #121a17;
      --bg-accent: #1f2b26;
      --surface: rgba(25, 35, 31, 0.88);
      --surface-alt: rgba(18, 26, 23, 0.7);
      --text: #eaf2ef;
      --muted: #a7b9b3;
      --line: rgba(202, 223, 216, 0.22);
      --brand: #58ad84;
      --brand-strong: #7bc39d;
      --warn: #e2a765;
      --danger: #f48e9a;
      --shadow: 0 18px 44px rgba(0, 0, 0, 0.44);
    }

    * {
      box-sizing: border-box;
    }

    body {
      margin: 0;
      font-family: "Space Grotesk", "Avenir Next", "Segoe UI", sans-serif;
      color: var(--text);
      background:
        radial-gradient(circle at 5% 10%, rgba(209, 216, 176, 0.58), rgba(209, 216, 176, 0) 30%),
        radial-gradient(circle at 92% 8%, rgba(247, 195, 128, 0.36), rgba(247, 195, 128, 0) 28%),
        linear-gradient(140deg, var(--bg), var(--bg-accent));
      min-height: 100vh;
    }

    .frame {
      max-width: 1200px;
      margin: 0 auto;
      padding: 26px 18px 44px;
      animation: reveal 320ms ease-out;
    }

    .header {
      display: flex;
      justify-content: space-between;
      gap: 14px;
      align-items: flex-start;
      flex-wrap: wrap;
      margin-bottom: 18px;
    }

    .title {
      margin: 0;
      font-family: "Fraunces", "Georgia", serif;
      font-size: clamp(2rem, 3.6vw, 2.9rem);
      line-height: 1;
      color: var(--brand-strong);
    }

    .subtitle {
      margin: 8px 0 0;
      color: var(--muted);
      max-width: 70ch;
    }

    .theme {
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--surface-alt);
      padding: 4px;
      display: inline-flex;
      gap: 4px;
      align-self: flex-start;
    }

    .theme-btn {
      border: 0;
      border-radius: 999px;
      background: transparent;
      color: var(--muted);
      padding: 6px 10px;
      cursor: pointer;
      font-size: 0.88rem;
      transition: background 130ms ease, color 130ms ease;
    }

    .theme-btn.active {
      color: #fff;
      background: linear-gradient(145deg, var(--brand), var(--brand-strong));
    }

    .status {
      display: flex;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
      padding: 11px 12px;
      border: 1px solid var(--line);
      border-radius: 12px;
      background: var(--surface-alt);
      margin-bottom: 16px;
    }

    .pill {
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 5px 10px;
      color: var(--muted);
      font-size: 0.9rem;
      background: var(--surface);
    }

    .status .actions {
      margin-left: auto;
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
    }

    .layout {
      display: grid;
      grid-template-columns: 1.15fr 1fr;
      gap: 14px;
    }

    .stack {
      display: grid;
      gap: 14px;
    }

    .card {
      border: 1px solid var(--line);
      border-radius: var(--radius);
      background: var(--surface);
      box-shadow: var(--shadow);
      padding: 16px;
      backdrop-filter: blur(4px);
    }

    .card h2 {
      margin: 0 0 12px;
      font-size: 1.14rem;
      letter-spacing: 0.25px;
    }

    .grid {
      display: grid;
      gap: 10px;
    }

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

    label {
      display: grid;
      gap: 5px;
      font-size: 0.9rem;
      color: var(--muted);
    }

    input, select, textarea, button {
      font: inherit;
    }

    input, select, textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 9px 10px;
      background: rgba(255, 255, 255, 0.75);
      color: var(--text);
      outline: none;
      transition: border-color 120ms ease, box-shadow 120ms ease;
    }

    :root[data-theme="dark"] input,
    :root[data-theme="dark"] select,
    :root[data-theme="dark"] textarea {
      background: rgba(16, 23, 20, 0.85);
    }

    input:focus, select:focus, textarea:focus {
      border-color: var(--brand);
      box-shadow: 0 0 0 3px rgba(88, 173, 132, 0.2);
    }

    textarea {
      min-height: 78px;
      resize: vertical;
      line-height: 1.33;
    }

    .check {
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 0.92rem;
    }

    .check input {
      width: auto;
      margin: 0;
    }

    .actions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      margin-top: 8px;
    }

    button, .btn {
      border: 1px solid transparent;
      border-radius: 10px;
      padding: 8px 12px;
      cursor: pointer;
      text-decoration: none;
      transition: transform 110ms ease, filter 110ms ease;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 36px;
    }

    button:hover, .btn:hover {
      transform: translateY(-1px);
      filter: brightness(1.03);
    }

    .primary {
      color: #fff;
      background: linear-gradient(145deg, var(--brand), var(--brand-strong));
    }

    .ghost {
      color: var(--brand-strong);
      background: var(--surface-alt);
      border-color: rgba(88, 173, 132, 0.3);
    }

    .warn {
      color: #fff;
      background: linear-gradient(145deg, #cf8f42, var(--warn));
    }

    .danger {
      color: #fff;
      background: linear-gradient(145deg, #cc5f6c, var(--danger));
    }

    .hint {
      color: var(--muted);
      font-size: 0.87rem;
      margin: 0;
    }

    .message {
      margin: 0;
      min-height: 1.2rem;
      color: var(--muted);
      font-size: 0.94rem;
    }

    .message.ok {
      color: var(--brand-strong);
    }

    .message.warn {
      color: var(--warn);
    }

    .message.err {
      color: var(--danger);
    }

    .stats {
      display: grid;
      gap: 8px;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      margin-top: 8px;
    }

    .stat {
      border: 1px solid var(--line);
      border-radius: 10px;
      background: var(--surface-alt);
      padding: 9px 10px;
    }

    .stat .k {
      margin: 0;
      text-transform: uppercase;
      letter-spacing: 0.18px;
      font-size: 0.75rem;
      color: var(--muted);
    }

    .stat .v {
      margin: 4px 0 0;
      font-size: 1rem;
      word-break: break-all;
    }

    .tree {
      margin-top: 8px;
      border: 1px solid var(--line);
      border-radius: 10px;
      background: var(--surface-alt);
      min-height: 220px;
      max-height: 360px;
      overflow: auto;
      padding: 10px;
      font-family: "IBM Plex Mono", "Cascadia Mono", "Menlo", monospace;
      font-size: 0.83rem;
      line-height: 1.45;
      white-space: pre;
    }

    .share-row {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 8px;
    }

    .preset-list {
      display: grid;
      gap: 10px;
      margin-top: 10px;
    }

    .preset {
      border: 1px solid var(--line);
      border-radius: 10px;
      background: var(--surface-alt);
      padding: 10px;
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
      font-size: 0.92rem;
      font-weight: 600;
      color: var(--text);
    }

    .small {
      font-size: 0.82rem;
      color: var(--muted);
    }

    @media (max-width: 980px) {
      .layout {
        grid-template-columns: 1fr;
      }
      .row-2, .row-3 {
        grid-template-columns: 1fr;
      }
      .stats {
        grid-template-columns: 1fr;
      }
      .share-row {
        grid-template-columns: 1fr;
      }
    }

    @keyframes reveal {
      from {
        opacity: 0;
        transform: translateY(8px);
      }
      to {
        opacity: 1;
        transform: translateY(0);
      }
    }
  </style>
</head>
<body>
  <main class="frame">
    <header class="header">
      <div>
        <h1 class="title">zip-forger</h1>
        <p class="subtitle">Build zip bundles from Forgejo repos by selecting presets or ad-hoc filters. Everything is streamed directly from the source repository.</p>
      </div>
      <div class="theme" role="group" aria-label="theme mode">
        <button class="theme-btn" type="button" data-theme="system">System</button>
        <button class="theme-btn" type="button" data-theme="light">Light</button>
        <button class="theme-btn" type="button" data-theme="dark">Dark</button>
      </div>
    </header>

    <section class="status">
      <span class="pill" id="authBadge">auth: checking</span>
      <span class="pill">ui: enabled</span>
      <div class="actions">
        <a class="btn ghost" id="loginBtn" href="/auth/login?return_to=/" hidden>Sign in</a>
        <button class="ghost" id="logoutBtn" type="button" hidden>Sign out</button>
      </div>
    </section>

    <div class="layout">
      <div class="stack">
        <section class="card">
          <h2>Selection</h2>
          <div class="grid">
            <div class="row-3">
              <label>Owner
                <input id="owner" list="ownerOptions" value="acme" autocomplete="off" />
                <datalist id="ownerOptions"></datalist>
              </label>
              <label>Repository
                <input id="repo" list="repoOptions" value="rules" autocomplete="off" />
                <datalist id="repoOptions"></datalist>
              </label>
              <label>Branch / Ref
                <input id="ref" list="branchOptions" value="main" autocomplete="off" />
                <datalist id="branchOptions"></datalist>
              </label>
            </div>

            <label>Preset
              <select id="preset">
                <option value="">(none)</option>
              </select>
            </label>
            <p class="hint" id="presetHint">Load repository config to populate presets.</p>

            <label class="check">
              <input id="useAdhoc" type="checkbox" />
              Enable ad-hoc filters
            </label>

            <div class="row-2">
              <label>Include globs
                <textarea id="includeGlobs" placeholder="rules/core/**/*.pdf"></textarea>
              </label>
              <label>Exclude globs
                <textarea id="excludeGlobs" placeholder="**/*draft*"></textarea>
              </label>
            </div>
            <div class="row-2">
              <label>Extensions
                <input id="extensions" placeholder=".pdf,.md" />
              </label>
              <label>Path prefixes
                <input id="prefixes" placeholder="rules/core,session-notes" />
              </label>
            </div>

            <div class="actions">
              <button class="ghost" id="loadConfigBtn" type="button">Load config</button>
              <button class="primary" id="previewBtn" type="button">Preview</button>
              <button class="warn" id="downloadBtn" type="button">Download ZIP</button>
            </div>

            <label>Shareable download URL
              <div class="share-row">
                <input id="shareUrl" readonly />
                <button class="ghost" id="copyUrlBtn" type="button">Copy URL</button>
              </div>
            </label>
          </div>
        </section>

        <section class="card">
          <h2>Config Editor</h2>
          <p class="hint">Edit zip-forger presets and save directly to the selected branch.</p>
          <div class="grid">
            <label class="check">
              <input id="allowAdhocFilters" type="checkbox" />
              Allow ad-hoc filters
            </label>
            <div class="actions">
              <button class="ghost" id="addPresetBtn" type="button">Add preset</button>
              <button class="primary" id="saveConfigBtn" type="button">Save config</button>
            </div>
            <div id="presetList" class="preset-list"></div>
          </div>
        </section>
      </div>

      <div class="stack">
        <section class="card">
          <h2>Preview</h2>
          <p id="message" class="message">Ready.</p>
          <div class="stats">
            <article class="stat">
              <p class="k">Commit</p>
              <p class="v" id="commitValue">-</p>
            </article>
            <article class="stat">
              <p class="k">Files</p>
              <p class="v" id="filesValue">0</p>
            </article>
            <article class="stat">
              <p class="k">Bytes</p>
              <p class="v" id="bytesValue">0 B</p>
            </article>
          </div>
          <div id="treeView" class="tree">No preview loaded.</div>
          <p id="treeHint" class="small"></p>
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
        preview: null
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
        loginBtn: document.getElementById("loginBtn"),
        logoutBtn: document.getElementById("logoutBtn"),
        themeButtons: Array.from(document.querySelectorAll("[data-theme]")),
        allowAdhocFilters: document.getElementById("allowAdhocFilters"),
        addPresetBtn: document.getElementById("addPresetBtn"),
        saveConfigBtn: document.getElementById("saveConfigBtn"),
        presetList: document.getElementById("presetList")
      };

      nodes.useAdhoc.checked = true;
      initTheme();
      wireEvents();
      hydrateAuth().then(() => initData());
      updateShareURL();

      function wireEvents() {
        nodes.loadConfigBtn.addEventListener("click", () => run(loadConfig));
        nodes.previewBtn.addEventListener("click", () => run(previewSelection));
        nodes.downloadBtn.addEventListener("click", triggerDownload);
        nodes.copyUrlBtn.addEventListener("click", copyShareURL);
        nodes.logoutBtn.addEventListener("click", logout);
        nodes.addPresetBtn.addEventListener("click", () => addPresetRow());
        nodes.saveConfigBtn.addEventListener("click", () => run(saveConfig));

        nodes.owner.addEventListener("change", () => run(onOwnerChanged));
        nodes.repo.addEventListener("change", () => run(onRepoChanged));
        nodes.ref.addEventListener("change", updateShareURL);
        nodes.preset.addEventListener("change", updateShareURL);
        nodes.useAdhoc.addEventListener("change", updateShareURL);
        nodes.includeGlobs.addEventListener("input", updateShareURL);
        nodes.excludeGlobs.addEventListener("input", updateShareURL);
        nodes.extensions.addEventListener("input", updateShareURL);
        nodes.prefixes.addEventListener("input", updateShareURL);

        nodes.themeButtons.forEach((button) => {
          button.addEventListener("click", () => setThemeMode(button.dataset.theme));
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

      async function hydrateAuth() {
        if (!AUTH_ENABLED) {
          nodes.authBadge.textContent = AUTH_REQUIRED ? "auth: required" : "auth: disabled";
          return;
        }

        nodes.loginBtn.hidden = false;
        nodes.loginBtn.href = "/auth/login?return_to=" + encodeURIComponent(window.location.pathname + window.location.search);

        try {
          const payload = await apiFetch("/auth/me", { credentials: "same-origin" });
          if (payload.authenticated) {
            nodes.authBadge.textContent = "auth: signed in";
            nodes.logoutBtn.hidden = false;
            nodes.loginBtn.hidden = true;
          } else {
            nodes.authBadge.textContent = AUTH_REQUIRED ? "auth: sign in required" : "auth: optional";
          }
        } catch (_err) {
          nodes.authBadge.textContent = "auth: unavailable";
        }
      }

      async function logout() {
        await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
        window.location.reload();
      }

      async function initData() {
        await loadOwners();
        await onOwnerChanged();
        await onRepoChanged();
        await loadConfig();
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

        nodes.commitValue.textContent = payload.commit || "-";
        nodes.allowAdhocFilters.checked = state.config.options.allowAdhocFilters !== false;
        nodes.useAdhoc.disabled = state.config.options.allowAdhocFilters === false;
        if (nodes.useAdhoc.disabled) {
          nodes.useAdhoc.checked = false;
        }

        renderPresetSelect();
        renderPresetEditor();
        nodes.presetHint.textContent = state.config.presets.length > 0
          ? "Loaded " + state.config.presets.length + " preset(s)."
          : "No presets found in .zip-forger.yaml.";
        setMessage("Config loaded.", "ok");
      }

      function renderPresetSelect() {
        const selected = nodes.preset.value;
        nodes.preset.innerHTML = "";
        nodes.preset.appendChild(optionNode("", "(none)"));
        for (const preset of state.config.presets) {
          const label = preset.description ? preset.id + " - " + preset.description : preset.id;
          nodes.preset.appendChild(optionNode(preset.id, label));
        }
        if (selected && state.config.presets.some((p) => p.id === selected)) {
          nodes.preset.value = selected;
        }
        updateShareURL();
      }

      function renderPresetEditor() {
        nodes.presetList.innerHTML = "";
        if (!state.config.presets.length) {
          const empty = document.createElement("p");
          empty.className = "small";
          empty.textContent = "No presets yet. Add one below.";
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
        deleteButton.className = "danger";
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
          throw new Error("No configuration loaded.");
        }

        state.config.version = 1;
        if (!state.config.options) {
          state.config.options = {};
        }
        state.config.options.allowAdhocFilters = nodes.allowAdhocFilters.checked;
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
        nodes.commitValue.textContent = payload.commit || "-";
        nodes.filesValue.textContent = String(payload.selectedFiles || 0);
        nodes.bytesValue.textContent = formatBytes(payload.totalBytes || 0);
        renderTree(payload.entries || [], !!payload.entriesTruncated);
        setMessage("Preview ready.", "ok");
      }

      function renderTree(paths, truncated) {
        if (!paths.length) {
          nodes.treeView.textContent = "(no files selected)";
          nodes.treeHint.textContent = "";
          return;
        }
        const lines = compactTreeLines(paths);
        nodes.treeView.textContent = lines.join("\n");
        nodes.treeHint.textContent = truncated
          ? "Preview list truncated for UI performance."
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
        const selected = currentSelection();
        if (!selected) {
          setMessage("Owner and repository are required.", "err");
          return;
        }
        window.location.assign(buildDownloadPath(selected));
      }

      async function copyShareURL() {
        const value = nodes.shareUrl.value;
        if (!value) {
          return;
        }
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(value);
        } else {
          nodes.shareUrl.focus();
          nodes.shareUrl.select();
          document.execCommand("copy");
        }
        setMessage("Download URL copied.", "ok");
      }

      function updateShareURL() {
        const selected = currentSelection();
        if (!selected) {
          nodes.shareUrl.value = "";
          return;
        }
        const absolute = window.location.origin + buildDownloadPath(selected);
        nodes.shareUrl.value = absolute;
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
        if (!value) {
          return "";
        }
        if (Array.isArray(value)) {
          return value.join(", ");
        }
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
