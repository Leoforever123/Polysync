const state = { data: null, modalMode: null, editingId: null, toastTimer: null };

const $ = (selector) => document.querySelector(selector);
const escapeHTML = (value = "") => String(value).replace(/[&<>'"]/g, char => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;"
})[char]);

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `请求失败 (${response.status})`);
  return body;
}

function toast(message, isError = false) {
  const node = $("#toast");
  node.textContent = message;
  node.className = `toast show${isError ? " error" : ""}`;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => node.className = "toast", 2800);
}

function formatTime(value) {
  if (!value || value.startsWith("0001-")) return "尚未同步";
  const date = new Date(value);
  const today = new Date();
  const sameDay = date.toDateString() === today.toDateString();
  return sameDay
    ? `今天 ${date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`
    : date.toLocaleString("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function statusText(share) {
  const status = share.status || {};
  if (status.state === "syncing") return status.message || "同步中…";
  if (status.state === "error") return status.message || "连接失败";
  if (status.lastSync && !status.lastSync.startsWith("0001-")) return `已同步 · ${formatTime(status.lastSync)}`;
  if (!share.peerAddress) return "等待另一台设备加入";
  return "等待首次同步";
}

function render() {
  if (!state.data) return;
  const { device, shares, activities } = state.data;
  const preferredAddress = device.addresses.find(item => !item.startsWith("127.")) || device.addresses[0];
  $("#device-card").innerHTML = `
    <div class="device-top">
      <div><div class="device-label">当前设备</div><div class="device-name">${escapeHTML(device.name)}</div></div>
      <div class="device-tools"><div class="platform">${escapeHTML(device.platform)}</div><button class="text-button edit-device">修改名称</button></div>
    </div>
    <div class="address-line"><span>${escapeHTML(preferredAddress)}</span><button class="text-button" data-copy="${escapeHTML(preferredAddress)}">复制地址</button></div>`;
  $("#share-count").textContent = `${shares.length} 个文件夹`;
  $("#share-list").innerHTML = shares.length ? shares.map(shareCard).join("") : emptyState();
  $("#activity-list").innerHTML = activities.length ? activities.slice(0, 10).map(activityRow).join("") : `<div class="no-activity">同步开始后，记录会显示在这里。</div>`;
}

function shareCard(share) {
  const status = share.status || { state: "idle" };
  const disabled = status.state === "syncing" ? "disabled" : "";
  return `<article class="share-card" data-share-id="${escapeHTML(share.id)}">
    <div class="share-header">
      <div class="folder-icon" aria-hidden="true"></div>
      <div class="card-actions">
        <button class="button small sync-button" ${disabled}>${status.state === "syncing" ? "同步中" : "立即同步"}</button>
        <button class="icon-button more edit-button" title="设置" aria-label="设置">•••</button>
      </div>
    </div>
    <div class="share-name">${escapeHTML(share.name)}</div>
    <div class="share-path" title="${escapeHTML(share.path)}">${escapeHTML(share.path)}</div>
    <div class="share-meta">
      <div class="status ${escapeHTML(status.state || "idle")}" title="${escapeHTML(status.message || "")}"><i class="status-dot"></i><span>${escapeHTML(statusText(share))}</span></div>
      <label class="auto-control"><span>自动</span><span class="switch"><input class="auto-toggle" type="checkbox" ${share.autoSync ? "checked" : ""}><i class="slider"></i></span></label>
    </div>
  </article>`;
}

function emptyState() {
  return `<div class="empty"><div><div class="empty-symbol">◇</div><h3>从第一个文件夹开始</h3><p>创建同步空间，或用另一台设备提供的同步码加入。</p><button class="button primary" id="empty-add">添加同步文件夹</button></div></div>`;
}

function activityRow(item) {
  return `<div class="activity-row"><i class="activity-mark ${escapeHTML(item.level)}"></i><span>${escapeHTML(item.message)}</span><time class="activity-time">${escapeHTML(formatTime(item.time))}</time></div>`;
}

async function refresh(silent = false) {
  try {
    state.data = await api("/api/status");
    $(".service-state").classList.remove("offline");
    $(".service-state").innerHTML = `<i></i>服务在线`;
    render();
  } catch (error) {
    if (!silent) toast(error.message, true);
    $(".service-state").classList.add("offline");
    $(".service-state").innerHTML = `<i></i>服务离线`;
  }
}

function openCreate(mode = "create") {
  state.modalMode = mode;
  state.editingId = null;
  $("#modal-content").innerHTML = createForm(mode);
  $("#modal-backdrop").classList.remove("hidden");
  $("#share-name").focus();
}

function createForm(mode) {
  const joining = mode === "join";
  return `<h2 id="modal-title">添加同步文件夹</h2>
    <p class="modal-intro">${joining ? "输入第一台设备生成的同步码，把本地文件夹接入同一同步空间。" : "先在这台设备创建，再把同步码交给另一台设备。"}</p>
    <div class="tabs"><button class="tab ${joining ? "" : "active"}" data-mode="create">创建新的</button><button class="tab ${joining ? "active" : ""}" data-mode="join">加入已有</button></div>
    <form id="share-form">
      ${joining ? `<div class="field"><label for="pair-code">同步码</label><input class="input" id="pair-code" required autocomplete="off" placeholder="粘贴同步码"><small>同步码包含访问密钥，请只发送给你信任的设备。</small></div>` : ""}
      <div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" maxlength="80" placeholder="例如：设计资料"></div>
      <div class="field"><label for="share-path">本地文件夹</label><div class="path-input"><input class="input" id="share-path" required placeholder="输入绝对路径"><button class="button ghost browse-button" type="button">浏览…</button></div></div>
      ${joining ? `<div class="field"><label for="peer-address">第一台设备地址</label><input class="input" id="peer-address" required placeholder="192.168.1.20:45123"><small>可在第一台设备右上方的当前设备卡片中找到。</small></div>` : ""}
      <div class="form-row">
        <label class="check-row"><input id="auto-sync" type="checkbox" checked> 在线时自动同步</label>
        <div class="field"><label for="interval">检查间隔</label><select class="select" id="interval"><option value="30">30 秒</option><option value="60">1 分钟</option><option value="300">5 分钟</option></select></div>
      </div>
      <div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">${joining ? "加入并同步" : "创建同步"}</button></div>
    </form>`;
}

function openEdit(id) {
  const share = state.data.shares.find(item => item.id === id);
  if (!share) return;
  state.editingId = id;
  state.modalMode = "edit";
  $("#modal-content").innerHTML = `<h2 id="modal-title">同步设置</h2><p class="modal-intro">配置连接和自动检查。修改设置不会移动或删除本地文件。</p>
    <form id="edit-form">
      <div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" maxlength="80" value="${escapeHTML(share.name)}"></div>
      <div class="field"><label for="share-path">本地文件夹</label><div class="path-input"><input class="input" id="share-path" value="${escapeHTML(share.path)}" required><button class="button ghost browse-button" type="button">浏览…</button></div></div>
      <div class="field"><label for="peer-address">对端地址</label><input class="input" id="peer-address" value="${escapeHTML(share.peerAddress || "")}" placeholder="192.168.1.20:45123"></div>
      <div class="form-row"><label class="check-row"><input id="auto-sync" type="checkbox" ${share.autoSync ? "checked" : ""}> 在线时自动同步</label><div class="field"><label for="interval">检查间隔（秒）</label><input class="input" id="interval" type="number" min="5" max="86400" value="${share.intervalSeconds}"></div></div>
      <div class="field"><label>同步码</label><div class="code-box">${escapeHTML(share.pairCode)}</div><button type="button" class="button small copy-code">复制同步码</button></div>
      <div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存设置</button></div>
      <div class="danger-zone"><div><strong>移除同步配置</strong><p>只移除配置，不会删除文件。</p></div><button type="button" class="button danger small delete-share">移除</button></div>
    </form>`;
  $("#modal-backdrop").classList.remove("hidden");
}

function openDeviceSettings() {
  state.modalMode = "device";
  $("#modal-content").innerHTML = `<h2 id="modal-title">设备名称</h2><p class="modal-intro">这个名称会显示在同步记录和冲突副本文件名中。</p>
    <form id="device-form"><div class="field"><label for="device-name">名称</label><input class="input" id="device-name" maxlength="80" required value="${escapeHTML(state.data.device.name)}"></div>
    <div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存</button></div></form>`;
  $("#modal-backdrop").classList.remove("hidden");
  $("#device-name").focus();
}

function showPairing(pairCode) {
  const addresses = state.data.device.addresses.filter(item => !item.startsWith("127."));
  $("#modal-content").innerHTML = `<h2 id="modal-title">同步空间已创建</h2><p class="modal-intro">在另一台设备打开 PolySync，选择“加入已有”，并填写下面的信息。</p>
    <label class="field"><strong>同步码</strong></label><div class="code-box">${escapeHTML(pairCode)}</div><button class="button small copy-value" data-value="${escapeHTML(pairCode)}">复制同步码</button>
    <div class="field pairing-addresses"><label>这台设备的局域网地址</label><div class="address-options">${addresses.length ? addresses.map(item => `<span class="address-chip">${escapeHTML(item)}</span>`).join("") : "<span class='muted'>未发现局域网 IPv4 地址</span>"}</div></div>
    <div class="warning">两台设备需要处于同一局域网，并允许 PolySync 通过系统防火墙。TCP 内容目前未加密，请只在可信网络使用。</div>
    <div class="modal-actions"><button class="button primary" type="button" data-close>完成</button></div>`;
}

function closeModal() {
  $("#modal-backdrop").classList.add("hidden");
  state.modalMode = null;
  state.editingId = null;
}

async function pickFolder(button) {
  button.disabled = true;
  try {
    const result = await api("/api/pick-folder", { method: "POST", body: "{}" });
    $("#share-path").value = result.path;
  } catch (error) { toast(error.message, true); }
  finally { button.disabled = false; }
}

function formPayload() {
  return {
    name: $("#share-name").value.trim(),
    path: $("#share-path").value.trim(),
    peerAddress: $("#peer-address")?.value.trim() || "",
    pairCode: $("#pair-code")?.value.trim() || "",
    autoSync: $("#auto-sync").checked,
    intervalSeconds: Number($("#interval").value)
  };
}

document.addEventListener("click", async event => {
  const target = event.target;
  if (target.closest("#add-share") || target.closest("#empty-add")) return openCreate();
  if (target.closest(".edit-device")) return openDeviceSettings();
  if (target.closest("#close-modal") || target.matches("[data-close]")) return closeModal();
  if (target.classList.contains("tab")) return openCreate(target.dataset.mode);
  if (target.classList.contains("browse-button")) return pickFolder(target);
  const copyTarget = target.closest("[data-copy]");
  if (copyTarget) { await navigator.clipboard.writeText(copyTarget.dataset.copy); return toast("地址已复制"); }
  const copyValue = target.closest(".copy-value");
  if (copyValue) { await navigator.clipboard.writeText(copyValue.dataset.value); return toast("同步码已复制"); }
  if (target.closest(".copy-code")) {
    const share = state.data.shares.find(item => item.id === state.editingId);
    await navigator.clipboard.writeText(share.pairCode); return toast("同步码已复制");
  }
  const card = target.closest(".share-card");
  if (target.closest(".edit-button") && card) return openEdit(card.dataset.shareId);
  if (target.closest(".sync-button") && card) {
    target.closest(".sync-button").disabled = true;
    try { await api(`/api/shares/${card.dataset.shareId}/sync`, { method: "POST", body: "{}" }); toast("已开始同步"); await refresh(true); }
    catch (error) { toast(error.message, true); }
    return;
  }
  if (target.closest(".delete-share")) {
    const share = state.data.shares.find(item => item.id === state.editingId);
    if (!confirm(`移除“${share.name}”的同步配置？本地文件不会被删除。`)) return;
    try { await api(`/api/shares/${share.id}`, { method: "DELETE" }); closeModal(); toast("同步配置已移除"); await refresh(); }
    catch (error) { toast(error.message, true); }
  }
});

document.addEventListener("change", async event => {
  if (!event.target.classList.contains("auto-toggle")) return;
  const card = event.target.closest(".share-card");
  const share = state.data.shares.find(item => item.id === card.dataset.shareId);
  try {
    await api(`/api/shares/${share.id}`, { method: "PUT", body: JSON.stringify({
      name: share.name, path: share.path, peerAddress: share.peerAddress || "", autoSync: event.target.checked, intervalSeconds: share.intervalSeconds
    }) });
    share.autoSync = event.target.checked;
    toast(event.target.checked ? "已开启自动同步" : "已关闭自动同步");
  } catch (error) { event.target.checked = !event.target.checked; toast(error.message, true); }
});

document.addEventListener("submit", async event => {
  event.preventDefault();
  const submit = event.target.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    if (event.target.id === "share-form") {
      const result = await api("/api/shares", { method: "POST", body: JSON.stringify(formPayload()) });
      await refresh(true);
      if (state.modalMode === "create") showPairing(result.pairCode);
      else { closeModal(); toast("已加入同步空间，正在等待同步"); setTimeout(() => triggerNewShare(result.id), 300); }
    } else if (event.target.id === "edit-form") {
      await api(`/api/shares/${state.editingId}`, { method: "PUT", body: JSON.stringify(formPayload()) });
      closeModal(); toast("设置已保存"); await refresh();
    } else if (event.target.id === "device-form") {
      await api("/api/settings", { method: "PUT", body: JSON.stringify({ deviceName: $("#device-name").value.trim() }) });
      closeModal(); toast("设备名称已更新"); await refresh();
    }
  } catch (error) { toast(error.message, true); submit.disabled = false; }
});

async function triggerNewShare(id) {
  try { await api(`/api/shares/${id}/sync`, { method: "POST", body: "{}" }); await refresh(true); }
  catch (error) { toast(error.message, true); }
}

$("#modal-backdrop").addEventListener("click", event => {
  if (event.target.id === "modal-backdrop") closeModal();
});
document.addEventListener("keydown", event => { if (event.key === "Escape") closeModal(); });

refresh();
setInterval(() => refresh(true), 2500);
