async function fetchJSON(url) {
  const res = await fetch(url, { headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`${url} -> ${res.status}`);
  return res.json();
}

async function fetchJSONWithOptions(url, options) {
  const res = await fetch(url, {
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    ...options,
  });
  let payload = {};
  try {
    payload = await res.json();
  } catch {
    payload = {};
  }
  if (!res.ok) {
    const errMsg = payload?.error || `${url} -> ${res.status}`;
    throw new Error(errMsg);
  }
  return payload;
}

function setText(id, value) {
  const el = document.getElementById(id);
  if (el) el.textContent = String(value);
}

function byId(id) {
  return document.getElementById(id);
}

function prettyJSON(obj) {
  return JSON.stringify(obj, null, 2);
}

const keyEditorState = {
  selectedHash: "",
  tokens: [],
};

const fallbackDocs = [
  {
    key: "quick-start",
    title: "Quick Start",
    body: [
      "Embedded docs are unavailable right now.",
      "Verify /assets/docs.json is bundled in the binary.",
    ],
  },
];
let embeddedDocs = [];

async function loadEmbeddedDocs() {
  try {
    const payload = await fetchJSON("/assets/docs.json");
    const docs = Array.isArray(payload) ? payload : payload?.docs;
    if (Array.isArray(docs) && docs.length > 0) {
      embeddedDocs = docs;
      return;
    }
    embeddedDocs = fallbackDocs;
  } catch {
    embeddedDocs = fallbackDocs;
  }
}

function setEditorStatus(text) {
  setText("keyEditorStatus", text);
}

function removeButton(onClick) {
  const btn = document.createElement("button");
  btn.textContent = "Remove";
  btn.addEventListener("click", onClick);
  return btn;
}

function parseCSVStrings(input) {
  return (input || "")
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}

function parseCSVInts(input) {
  return parseCSVStrings(input)
    .map((v) => Number(v))
    .filter((v) => Number.isFinite(v));
}

function toRFC3339FromLocalInput(localValue) {
  if (!localValue) return "";
  const dt = new Date(localValue);
  if (Number.isNaN(dt.getTime())) return "";
  return dt.toISOString();
}

function toLocalInputFromRFC3339(rfc3339Value) {
  if (!rfc3339Value) return "";
  const dt = new Date(rfc3339Value);
  if (Number.isNaN(dt.getTime())) return "";
  const pad = (n) => String(n).padStart(2, "0");
  const yyyy = dt.getFullYear();
  const mm = pad(dt.getMonth() + 1);
  const dd = pad(dt.getDate());
  const hh = pad(dt.getHours());
  const min = pad(dt.getMinutes());
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`;
}

function addPromptRow(prompt = { role: "system", content: "" }) {
  const rows = byId("promptRows");
  const row = document.createElement("div");
  row.className = "row-card";
  const inline = document.createElement("div");
  inline.className = "inline";
  const roleLabel = document.createElement("label");
  roleLabel.textContent = "Role";
  const roleSelect = document.createElement("select");
  roleSelect.setAttribute("data-k", "role_select");
  ["system", "developer", "user", "assistant", "custom"].forEach((value) => {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    roleSelect.appendChild(option);
  });
  roleLabel.appendChild(roleSelect);

  const roleCustomLabel = document.createElement("label");
  roleCustomLabel.textContent = "Custom role";
  const roleCustomInput = document.createElement("input");
  roleCustomInput.setAttribute("data-k", "role_custom");
  roleCustomInput.type = "text";
  roleCustomInput.placeholder = "custom role";
  roleCustomLabel.appendChild(roleCustomInput);

  const initialRole = (prompt.role || "system").trim();
  const knownRoles = ["system", "developer", "user", "assistant"];
  if (knownRoles.includes(initialRole)) {
    roleSelect.value = initialRole;
    roleCustomInput.value = "";
    roleCustomInput.disabled = true;
  } else {
    roleSelect.value = "custom";
    roleCustomInput.value = initialRole;
    roleCustomInput.disabled = false;
  }
  roleSelect.addEventListener("change", () => {
    const isCustom = roleSelect.value === "custom";
    roleCustomInput.disabled = !isCustom;
    if (!isCustom) {
      roleCustomInput.value = "";
    }
  });
  inline.appendChild(roleLabel);
  inline.appendChild(roleCustomLabel);
  const contentLabel = document.createElement("label");
  contentLabel.style.gridColumn = "1 / -1";
  contentLabel.textContent = "Content";
  const contentArea = document.createElement("textarea");
  contentArea.setAttribute("data-k", "content");
  contentArea.setAttribute("rows", "5");
  contentArea.value = prompt.content || "";
  contentLabel.appendChild(contentArea);
  inline.appendChild(removeButton(() => row.remove()));
  row.appendChild(inline);
  row.appendChild(contentLabel);
  rows.appendChild(row);
}

function addRuleRow(rule = {}) {
  const rows = byId("ruleRows");
  const row = document.createElement("div");
  row.className = "row-card";
  row.innerHTML = `
    <div class="inline">
      <label>Name <input data-k="name" type="text" value="${rule.name || ""}" /></label>
      <label>Type <input data-k="type" type="text" value="${rule.type || "regex"}" placeholder="regex|keyword|pii|jailbreak" /></label>
      <label>Action <input data-k="action" type="text" value="${rule.action || "fail"}" placeholder="fail|warn|log|mask" /></label>
      <label>Scope <input data-k="scope" type="text" value="${rule.scope || "input"}" placeholder="input|output|both" /></label>
    </div>
    <div class="inline">
      <label style="grid-column: span 2;">Pattern <input data-k="pattern" type="text" value="${rule.pattern || ""}" /></label>
      <label>Keywords (csv) <input data-k="keywords" type="text" value="${(rule.keywords || []).join(",")}" /></label>
      <label>Detect (csv) <input data-k="detect" type="text" value="${(rule.detect || []).join(",")}" /></label>
    </div>
  `;
  row.querySelector(".inline:last-child").appendChild(removeButton(() => row.remove()));
  rows.appendChild(row);
}

function addRateRuleRow(rule = {}) {
  const rows = byId("rateRuleRows");
  const row = document.createElement("div");
  row.className = "row-card";
  const inline = document.createElement("div");
  inline.className = "inline";
  inline.innerHTML = `
    <label>Requests <input data-k="requests" type="number" min="0" value="${rule.requests ?? ""}" /></label>
    <label>Tokens <input data-k="tokens" type="number" min="0" value="${rule.tokens ?? ""}" /></label>
    <label>Window <input data-k="window" type="text" value="${rule.window || ""}" placeholder="1m" /></label>
    <label>Strategy <input data-k="strategy" type="text" value="${rule.strategy || ""}" placeholder="sliding|fixed" /></label>
  `;
  inline.appendChild(removeButton(() => row.remove()));
  row.appendChild(inline);
  rows.appendChild(row);
}

function addMetadataRow(entry = { key: "", value: "" }) {
  const rows = byId("metadataRows");
  const row = document.createElement("div");
  row.className = "row-card";
  const inline = document.createElement("div");
  inline.className = "inline";
  inline.innerHTML = `
    <label>Key <input data-k="key" type="text" value="${entry.key || ""}" /></label>
    <label style="grid-column: span 3;">Value <input data-k="value" type="text" value="${entry.value || ""}" /></label>
  `;
  inline.appendChild(removeButton(() => row.remove()));
  row.appendChild(inline);
  rows.appendChild(row);
}

function addProviderRow(entry = { provider: "", policy: {} }) {
  const rows = byId("providerRows");
  const row = document.createElement("div");
  row.className = "row-card";
  row.innerHTML = `
    <div class="inline">
      <label>Provider name <input data-k="provider" type="text" value="${entry.provider || ""}" placeholder="openai" /></label>
    </div>
    <label>Provider policy JSON
      <textarea data-k="policy" rows="8">${prettyJSON(entry.policy || {})}</textarea>
    </label>
  `;
  row.querySelector(".inline").appendChild(removeButton(() => row.remove()));
  rows.appendChild(row);
}

function collectRows(selector, parser) {
  return Array.from(document.querySelectorAll(selector)).map(parser);
}

function nonEmptyNumber(input) {
  const t = String(input ?? "").trim();
  if (!t) return undefined;
  const n = Number(t);
  return Number.isFinite(n) ? n : undefined;
}

function policyFromForm() {
  const policy = {};
  const setStr = (k, id) => {
    const v = byId(id).value.trim();
    if (v) policy[k] = v;
  };
  setStr("base_key_env", "keyBaseEnv");
  setStr("upstream_url", "keyUpstreamUrl");
  setStr("default_provider", "keyDefaultProvider");
  setStr("model", "keyModel");
  setStr("model_regex", "keyModelRegex");

  const maxTokens = nonEmptyNumber(byId("keyMaxTokens").value);
  if (maxTokens !== undefined) policy.max_tokens = maxTokens;
  const timeout = nonEmptyNumber(byId("keyTimeout").value);
  if (timeout !== undefined) policy.timeout = timeout;

  const prompts = collectRows("#promptRows .row-card", (row) => {
    const selectedRole = row.querySelector('[data-k="role_select"]').value.trim();
    const customRole = row.querySelector('[data-k="role_custom"]').value.trim();
    const role = selectedRole === "custom" ? customRole : selectedRole;
    return {
      role,
      content: row.querySelector('[data-k="content"]').value.trim(),
    };
  }).filter((p) => p.role || p.content);
  if (prompts.length) policy.prompts = prompts;

  const rules = collectRows("#ruleRows .row-card", (row) => ({
    name: row.querySelector('[data-k="name"]').value.trim(),
    type: row.querySelector('[data-k="type"]').value.trim(),
    action: row.querySelector('[data-k="action"]').value.trim(),
    scope: row.querySelector('[data-k="scope"]').value.trim(),
    pattern: row.querySelector('[data-k="pattern"]').value.trim(),
    keywords: parseCSVStrings(row.querySelector('[data-k="keywords"]').value),
    detect: parseCSVStrings(row.querySelector('[data-k="detect"]').value),
  })).filter((r) => r.type);
  if (rules.length) policy.rules = rules;

  const maxParallel = nonEmptyNumber(byId("rateMaxParallel").value);
  const rateRules = collectRows("#rateRuleRows .row-card", (row) => ({
    requests: nonEmptyNumber(row.querySelector('[data-k="requests"]').value),
    tokens: nonEmptyNumber(row.querySelector('[data-k="tokens"]').value),
    window: row.querySelector('[data-k="window"]').value.trim(),
    strategy: row.querySelector('[data-k="strategy"]').value.trim(),
  })).filter((rr) => rr.requests !== undefined || rr.tokens !== undefined || rr.window || rr.strategy);
  if (maxParallel !== undefined || rateRules.length) {
    policy.rate_limit = {};
    if (maxParallel !== undefined) policy.rate_limit.max_parallel = maxParallel;
    if (rateRules.length) policy.rate_limit.rules = rateRules;
  }

  const retryMax = nonEmptyNumber(byId("retryMaxRetries").value);
  const retryOn = parseCSVInts(byId("retryRetryOn").value);
  const fallbacks = parseCSVStrings(byId("retryFallbacks").value);
  if (retryMax !== undefined || retryOn.length || fallbacks.length) {
    policy.retry = {};
    if (retryMax !== undefined) policy.retry.max_retries = retryMax;
    if (retryOn.length) policy.retry.retry_on = retryOn;
    if (fallbacks.length) policy.retry.fallbacks = fallbacks;
  }

  const metadataEntries = collectRows("#metadataRows .row-card", (row) => ({
    key: row.querySelector('[data-k="key"]').value.trim(),
    value: row.querySelector('[data-k="value"]').value.trim(),
  })).filter((m) => m.key);
  if (metadataEntries.length) {
    policy.metadata = {};
    metadataEntries.forEach((m) => { policy.metadata[m.key] = m.value; });
  }

  const providerEntries = collectRows("#providerRows .row-card", (row) => {
    const provider = row.querySelector('[data-k="provider"]').value.trim();
    const policyJSON = row.querySelector('[data-k="policy"]').value.trim();
    return { provider, policyJSON };
  }).filter((p) => p.provider && p.policyJSON);
  if (providerEntries.length) {
    policy.providers = {};
    providerEntries.forEach((entry) => {
      const parsed = JSON.parse(entry.policyJSON);
      if (!policy.providers[entry.provider]) policy.providers[entry.provider] = [];
      policy.providers[entry.provider].push(parsed);
    });
  }

  return policy;
}

function populateFormFromPolicy(policy = {}) {
  byId("keyBaseEnv").value = policy.base_key_env || "";
  byId("keyUpstreamUrl").value = policy.upstream_url || "";
  byId("keyDefaultProvider").value = policy.default_provider || "";
  byId("keyMaxTokens").value = policy.max_tokens ?? "";
  byId("keyModel").value = policy.model || "";
  byId("keyModelRegex").value = policy.model_regex || "";
  byId("keyTimeout").value = policy.timeout ?? "";

  byId("promptRows").innerHTML = "";
  (policy.prompts || []).forEach(addPromptRow);
  byId("ruleRows").innerHTML = "";
  (policy.rules || []).forEach(addRuleRow);
  byId("rateMaxParallel").value = policy.rate_limit?.max_parallel ?? "";
  byId("rateRuleRows").innerHTML = "";
  (policy.rate_limit?.rules || []).forEach(addRateRuleRow);
  byId("retryMaxRetries").value = policy.retry?.max_retries ?? "";
  byId("retryRetryOn").value = (policy.retry?.retry_on || []).join(",");
  byId("retryFallbacks").value = (policy.retry?.fallbacks || []).join(",");

  byId("metadataRows").innerHTML = "";
  Object.entries(policy.metadata || {}).forEach(([key, value]) => addMetadataRow({ key, value }));

  byId("providerRows").innerHTML = "";
  Object.entries(policy.providers || {}).forEach(([provider, entries]) => {
    (entries || []).forEach((entry) => addProviderRow({ provider, policy: entry }));
  });

  byId("keyPolicyJson").value = prettyJSON(policy);
}

function syncFormToJson() {
  try {
    const policy = policyFromForm();
    byId("keyPolicyJson").value = prettyJSON(policy);
    setEditorStatus("Generated JSON from form.");
  } catch (err) {
    setEditorStatus(`Generate JSON failed: ${err.message}`);
  }
}

function syncJsonToForm() {
  try {
    const raw = byId("keyPolicyJson").value.trim() || "{}";
    const policy = JSON.parse(raw);
    populateFormFromPolicy(policy);
    setEditorStatus("Loaded JSON into form.");
  } catch (err) {
    setEditorStatus(`Load JSON failed: ${err.message}`);
  }
}

function testModelRegex() {
  const pattern = byId("keyModelRegex").value.trim();
  if (!pattern) {
    setEditorStatus("No model regex set.");
    return;
  }
  const value = window.prompt("Enter a model name to test against the current regex:", "gpt-4o-mini");
  if (value === null) {
    return;
  }
  try {
    const re = new RegExp(pattern);
    const matched = re.test(value);
    setEditorStatus(matched ? `MATCH: /${pattern}/ matches "${value}"` : `NO MATCH: /${pattern}/ does not match "${value}"`);
  } catch (err) {
    setEditorStatus(`Invalid regex: ${err.message}`);
  }
}

function showCreatedKeyModal(keyValue) {
  const modal = byId("keyModal");
  byId("keyModalValue").textContent = keyValue || "";
  modal.classList.remove("hidden");
}

function hideCreatedKeyModal() {
  byId("keyModal").classList.add("hidden");
}

function openPolicyEditorModal() {
  byId("policyEditorModal").classList.remove("hidden");
}

function closePolicyEditorModal() {
  byId("policyEditorModal").classList.add("hidden");
}

function resetKeyEditor() {
  keyEditorState.selectedHash = "";
  byId("keyEditorTitle").textContent = "Create key";
  byId("keyExpires").value = "";
  populateFormFromPolicy({
    rules: [],
    prompts: [],
    providers: {},
  });
  setEditorStatus("Ready.");
}

function populateEditorFromToken(token, detail) {
  keyEditorState.selectedHash = token.token_hash;
  byId("keyEditorTitle").textContent = `View key: ${token.token_hash}`;
  byId("keyExpires").value = toLocalInputFromRFC3339(token.expires_at || "");
  const parsed = detail?.policy_raw ? JSON.parse(detail.policy_raw) : {};
  populateFormFromPolicy(parsed);
  setEditorStatus(`Loaded ${token.token_hash}`);
}

async function selectKeyForEdit(token) {
  const detail = await fetchJSON(`/admin/api/tokens/${token.token_hash}`);
  populateEditorFromToken(token, detail);
  openPolicyEditorModal();
}

function setEnvVarOptions(data) {
  const list = byId("envVarOptions");
  list.innerHTML = "";
  (data?.env_vars || []).forEach((name) => {
    const option = document.createElement("option");
    option.value = name;
    list.appendChild(option);
  });
}

function renderKeys(tokens) {
  keyEditorState.tokens = tokens || [];
  const tbody = document.querySelector("#keysTable tbody");
  tbody.innerHTML = "";
  for (const token of tokens) {
    const providers = (token.providers || []).map((p) => p.name).join(", ");
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${token.token_hash}</td>
      <td>${token.created_at || ""}</td>
      <td>${token.expires_at || "-"}</td>
      <td>${token.policy?.default_provider || "-"}</td>
      <td>${token.policy?.max_tokens || "-"}</td>
      <td>${providers || "-"}</td>
      <td></td>
    `;
    const actionsCell = tr.querySelector("td:last-child");
    const viewBtn = document.createElement("button");
    viewBtn.textContent = "View";
    viewBtn.addEventListener("click", async () => {
      try {
        await selectKeyForEdit(token);
      } catch (err) {
        setEditorStatus(`Load failed: ${err.message}`);
      }
    });
    const deleteBtn = document.createElement("button");
    deleteBtn.textContent = "Delete";
    deleteBtn.addEventListener("click", async () => {
      if (!confirm(`Delete key ${token.token_hash}?`)) return;
      try {
        await fetchJSONWithOptions(`/admin/api/keys/${token.token_hash}`, { method: "DELETE" });
        setEditorStatus(`Deleted ${token.token_hash}`);
        await refreshKeys();
      } catch (err) {
        setEditorStatus(`Delete failed: ${err.message}`);
      }
    });
    actionsCell.appendChild(viewBtn);
    actionsCell.appendChild(deleteBtn);
    tbody.appendChild(tr);
  }
}

function renderSessions(data) {
  const summary = data?.summary || {};
  setText("sessionSummary", `Sessions: ${summary.sessions || 0} | Requests: ${summary.request_count || 0} | Total Tokens: ${summary.total_tokens || 0}`);
  const tbody = document.querySelector("#sessionsTable tbody");
  tbody.innerHTML = "";
  for (const s of data?.sessions || []) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${s.session_id || ""}</td>
      <td>${s.started_at || ""}</td>
      <td>${s.totals?.request_count || 0}</td>
      <td>${s.totals?.total_tokens || 0}</td>
      <td>${s.git?.branch || "-"}</td>
    `;
    tbody.appendChild(tr);
  }
}

function renderMemoryFiles(files) {
  const list = document.getElementById("memoryList");
  list.innerHTML = "";
  for (const file of files || []) {
    const li = document.createElement("li");
    const btn = document.createElement("button");
    btn.textContent = file.path;
    btn.addEventListener("click", async () => {
      const data = await fetchJSON(`/admin/api/memory/files/${file.path}`);
      setText("memoryContent", data.content || "");
    });
    li.appendChild(btn);
    list.appendChild(li);
  }
}

function renderDocsList() {
  const list = byId("docsList");
  list.innerHTML = "";
  embeddedDocs.forEach((doc) => {
    const li = document.createElement("li");
    const btn = document.createElement("button");
    btn.textContent = doc.title;
    btn.addEventListener("click", () => renderDoc(doc.key));
    li.appendChild(btn);
    list.appendChild(li);
  });
}

function renderDoc(key) {
  const doc = embeddedDocs.find((d) => d.key === key) || embeddedDocs[0];
  byId("docsTitle").textContent = doc.title;
  const body = byId("docsBody");
  body.innerHTML = "";
  const ul = document.createElement("ul");
  doc.body.forEach((line) => {
    if (line.startsWith("- ")) {
      const li = document.createElement("li");
      li.textContent = line.slice(2);
      ul.appendChild(li);
    } else {
      if (ul.childElementCount > 0) {
        body.appendChild(ul.cloneNode(true));
        ul.innerHTML = "";
      }
      const p = document.createElement("p");
      p.textContent = line;
      body.appendChild(p);
    }
  });
  if (ul.childElementCount > 0) {
    body.appendChild(ul);
  }
}

function initTabs() {
  const buttons = document.querySelectorAll(".nav-btn");
  buttons.forEach((btn) => {
    btn.addEventListener("click", () => {
      buttons.forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      document.querySelectorAll(".tab").forEach((tab) => tab.classList.remove("active"));
      const tab = document.getElementById(`tab-${btn.dataset.tab}`);
      if (tab) tab.classList.add("active");
    });
  });
}

async function refreshKeys() {
  const keysResp = await fetchJSON("/admin/api/keys");
  renderKeys(keysResp.tokens || []);
}

async function saveKeyFromEditor() {
  let policy;
  try {
    policy = policyFromForm();
    byId("keyPolicyJson").value = prettyJSON(policy);
  } catch (err) {
    setEditorStatus(`Validation failed: ${err.message}`);
    return;
  }
  const expires = toRFC3339FromLocalInput(byId("keyExpires").value.trim());
  const body = {
    policy: JSON.stringify(policy),
    expires,
  };
  try {
    if (keyEditorState.selectedHash) {
      await fetchJSONWithOptions(`/admin/api/keys/${keyEditorState.selectedHash}`, {
        method: "PUT",
        body: JSON.stringify(body),
      });
      setEditorStatus(`Updated ${keyEditorState.selectedHash}`);
      closePolicyEditorModal();
    } else {
      const created = await fetchJSONWithOptions("/admin/api/keys", {
        method: "POST",
        body: JSON.stringify(body),
      });
      setEditorStatus(`Created key.\nHash: ${created.hash}\nToken (only shown once): ${created.token}`);
      showCreatedKeyModal(created.token || "");
      keyEditorState.selectedHash = created.hash || "";
      if (created.hash) {
        byId("keyEditorTitle").textContent = `View key: ${created.hash}`;
      }
      closePolicyEditorModal();
    }
    await refreshKeys();
  } catch (err) {
    setEditorStatus(`Save failed: ${err.message}`);
  }
}

async function deleteSelectedKey() {
  if (!keyEditorState.selectedHash) {
    setEditorStatus("No selected key to delete.");
    return;
  }
  if (!confirm(`Delete key ${keyEditorState.selectedHash}?`)) return;
  try {
    await fetchJSONWithOptions(`/admin/api/keys/${keyEditorState.selectedHash}`, { method: "DELETE" });
    setEditorStatus(`Deleted ${keyEditorState.selectedHash}`);
    resetKeyEditor();
    closePolicyEditorModal();
    await refreshKeys();
  } catch (err) {
    setEditorStatus(`Delete failed: ${err.message}`);
  }
}

async function boot() {
  try {
    initTabs();
    const health = await fetchJSON("/admin/api/health");
    setText("healthBadge", `${health.status.toUpperCase()} ${health.tls_enabled ? "TLS" : "HTTP"}`);

    const analytics = await fetchJSON("/admin/api/analytics/summary");
    const totals = analytics?.totals || {};
    setText("mRequests", totals.request_count || 0);
    setText("mInput", totals.input_tokens || 0);
    setText("mOutput", totals.output_tokens || 0);
    setText("mTotal", totals.total_tokens || 0);
    setText("mErrors", totals.error_count || 0);
    setText("mSessions", analytics?.ledger?.sessions || 0);

    const envVars = await fetchJSON("/admin/api/env-vars");
    setEnvVarOptions(envVars);
    resetKeyEditor();
    await refreshKeys();
    byId("refreshKeys").addEventListener("click", refreshKeys);
    byId("newKeyBtn").addEventListener("click", () => {
      resetKeyEditor();
      openPolicyEditorModal();
    });
    byId("saveKeyBtn").addEventListener("click", saveKeyFromEditor);
    byId("deleteKeyBtn").addEventListener("click", deleteSelectedKey);
    byId("clearKeyBtn").addEventListener("click", resetKeyEditor);
    byId("addPromptBtn").addEventListener("click", () => addPromptRow());
    byId("addRuleBtn").addEventListener("click", () => addRuleRow());
    byId("addRateRuleBtn").addEventListener("click", () => addRateRuleRow());
    byId("addMetadataBtn").addEventListener("click", () => addMetadataRow());
    byId("addProviderBtn").addEventListener("click", () => addProviderRow());
    byId("syncJsonToFormBtn").addEventListener("click", syncJsonToForm);
    byId("syncFormToJsonBtn").addEventListener("click", syncFormToJson);
    byId("testModelRegexBtn").addEventListener("click", testModelRegex);
    byId("closePolicyEditorBtn").addEventListener("click", closePolicyEditorModal);
    byId("policyEditorModal").querySelector(".modal-backdrop").addEventListener("click", closePolicyEditorModal);
    byId("closeKeyModalBtn").addEventListener("click", hideCreatedKeyModal);
    byId("copyKeyModalBtn").addEventListener("click", async () => {
      const text = byId("keyModalValue").textContent || "";
      try {
        await navigator.clipboard.writeText(text);
        setEditorStatus("Key copied to clipboard.");
      } catch (err) {
        setEditorStatus(`Copy failed: ${err.message}`);
      }
    });
    byId("keyModal").querySelector(".modal-backdrop").addEventListener("click", hideCreatedKeyModal);

    const sessions = await fetchJSON("/admin/api/sessions");
    renderSessions(sessions);

    const memory = await fetchJSON("/admin/api/memory/files");
    renderMemoryFiles(memory.files || []);
    await loadEmbeddedDocs();
    renderDocsList();
    renderDoc("quick-start");
  } catch (err) {
    setText("healthBadge", "ERROR");
    console.error(err);
  }
}

boot();
