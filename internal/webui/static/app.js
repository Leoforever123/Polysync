const state = { data: null, modal: null, selectedId: null, pairSession: null, toastTimer: null };
const $ = selector => document.querySelector(selector);
const escapeHTML = (value = "") => String(value).replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);

async function api(path, options = {}) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json", ...(options.headers || {}) }, ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `请求失败 (${response.status})`);
  return body;
}

function toast(message, error = false) {
  const node = $("#toast");
  node.textContent = message;
  node.className = `toast show${error ? " error" : ""}`;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => node.className = "toast", 2800);
}

function formatTime(value) {
  if (!value || value.startsWith("0001-")) return "从未";
  const date = new Date(value);
  return date.toDateString() === new Date().toDateString() ? `今天 ${date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}` : date.toLocaleString("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

async function refresh(silent = false) {
  try {
    state.data = await api("/api/status");
    $(".service-state").classList.remove("offline");
    $(".service-state").innerHTML = `<i></i>服务在线`;
    render();
  } catch (error) {
    $(".service-state").classList.add("offline");
    $(".service-state").innerHTML = `<i></i>服务离线`;
    if (!silent) toast(error.message, true);
  }
}

function render() {
  const { device, shares, activities } = state.data;
  const address = device.addresses.find(item => !item.startsWith("127.")) || device.addresses[0];
  $("#device-card").innerHTML = `<div class="device-top"><div><div class="device-label">当前设备</div><div class="device-name">${escapeHTML(device.name)}</div></div><div class="device-tools"><div class="platform">${escapeHTML(device.platform)}</div><button class="text-button edit-device">修改名称</button></div></div><div class="address-line"><span>${escapeHTML(address)}</span><button class="text-button" data-copy="${escapeHTML(address)}">复制地址</button></div>`;
  renderAttention();
  renderDevices();
  $("#share-count").textContent = `${shares.length} 个文件夹`;
  $("#share-list").innerHTML = shares.length ? shares.map(shareCard).join("") : emptyShares();
  renderConflicts();
  $("#activity-list").innerHTML = activities.length ? activities.slice(0, 10).map(activityRow).join("") : `<div class="no-activity">设备配对或同步开始后，记录会显示在这里。</div>`;
}

function renderAttention() {
  const items = [];
  (state.data.pairingRequests || []).forEach(request => items.push(`<div class="attention-card"><div><strong>${escapeHTML(request.deviceName)} 请求配对</strong><p>${request.code ? "将验证码告诉发起设备，60 秒内有效。" : "确认设备名称无误后允许本次请求。"}</p></div>${request.code ? `<div class="pair-code">${escapeHTML(request.code)}</div>` : `<div class="choice-actions"><button class="button small approve-pair" data-id="${escapeHTML(request.id)}">允许</button><button class="button small danger reject-pair" data-id="${escapeHTML(request.id)}">拒绝</button></div>`}</div>`));
  (state.data.shareInvitations || []).forEach(invite => items.push(`<div class="attention-card"><div><strong>${escapeHTML(invite.deviceName)} 邀请同步“${escapeHTML(invite.name)}”</strong><p>选择一个本地文件夹即可加入，不需要再次验证身份。</p></div><button class="button small accept-invite" data-id="${escapeHTML(invite.id)}">选择文件夹</button></div>`));
  $("#attention-list").innerHTML = items.join("");
}

function renderDevices() {
  const paired = state.data.pairedDevices || [];
  const nearby = (state.data.nearbyDevices || []).filter(device => !device.paired);
  $("#device-count").textContent = `${paired.length} 台已配对 · ${nearby.length} 台附近设备`;
  const cards = [...paired.map(device => peerCard(device, true)), ...nearby.map(device => peerCard(device, false))];
  $("#device-list").innerHTML = cards.length ? cards.join("") : `<div class="empty"><div><div class="empty-symbol">⌁</div><h3>正在发现附近设备</h3><p>确保两台设备位于同一局域网并已启动 PolySync。</p></div></div>`;
}

function peerCard(device, paired) {
  const online = Boolean(device.online);
  const fingerprint = device.fingerprint || (device.publicKey ? device.publicKey.slice(0, 16) : "");
  return `<article class="peer-card"><div class="peer-top"><div><div class="peer-name">${escapeHTML(device.name)}</div><div class="peer-detail" title="${escapeHTML(fingerprint)}">${escapeHTML(fingerprint)}</div></div><span class="peer-state ${online ? "online" : ""}"><i></i>${online ? "在线" : "离线"}</span></div><div class="peer-footer">${paired ? `<span class="trust-badge">已安全配对</span><span class="muted">${escapeHTML(formatTime(device.lastSeen))}</span>` : `<span class="muted">附近设备</span><button class="button small pair-device" data-id="${escapeHTML(device.id)}" data-address="${escapeHTML((device.addresses || [])[0] || "")}">配对</button>`}</div></article>`;
}

function statusText(share) {
  if (share.state === "pending") return "等待对方接受邀请";
  if (share.state === "legacy") return "需要重新配对并邀请";
  const status = share.status || {};
  if (status.state === "syncing") return status.message || "同步中…";
  if (status.state === "error") return status.message || "同步失败";
  if (status.lastSync && !status.lastSync.startsWith("0001-")) return `已同步 · ${formatTime(status.lastSync)}`;
  return "等待首次同步";
}

function shareCard(share) {
  const status = share.status || { state: "idle" };
  const active = share.state === "active";
  return `<article class="share-card" data-share-id="${escapeHTML(share.id)}"><div class="share-header"><div class="folder-icon"></div><div class="card-actions"><button class="button small sync-button" ${!active || status.state === "syncing" ? "disabled" : ""}>${status.state === "syncing" ? "同步中" : "立即同步"}</button><button class="icon-button more edit-button" aria-label="设置">•••</button></div></div><div class="share-name">${escapeHTML(share.name)}</div><div class="share-path" title="${escapeHTML(share.path)}">${escapeHTML(share.path)}</div><div class="share-meta"><div class="status ${escapeHTML(status.state || "idle")}"><i class="status-dot"></i><span>${escapeHTML(statusText(share))}</span></div><label class="auto-control"><span>自动</span><span class="switch"><input class="auto-toggle" type="checkbox" ${share.autoSync ? "checked" : ""} ${!active ? "disabled" : ""}><i class="slider"></i></span></label></div></article>`;
}

function emptyShares() {
  return `<div class="empty"><div><div class="empty-symbol">◇</div><h3>从第一个同步文件夹开始</h3><p>先配对一台设备，然后发送文件夹同步邀请。</p><button class="button primary" id="empty-add">添加同步文件夹</button></div></div>`;
}

function renderConflicts() {
  const conflicts = (state.data.conflicts || []).filter(item => item.status === "pending");
  $("#conflict-section").classList.toggle("hidden", conflicts.length === 0);
  $("#conflict-count").textContent = `${conflicts.length} 个待解决`;
  $("#conflict-list").innerHTML = conflicts.map(item => `<div class="conflict-row"><div><div class="conflict-path">${escapeHTML(item.path)}</div><div class="conflict-kind">${escapeHTML(conflictKind(item.kind))} · ${escapeHTML(item.localDevice)} / ${escapeHTML(item.remoteDevice)}</div></div><button class="button small resolve-conflict" data-id="${escapeHTML(item.id)}">查看并合并</button></div>`).join("");
}

function conflictKind(kind) {
  return ({ "modify-modify": "双方都修改", "add-add": "双方同名新建", "modify-delete": "本机修改、对端删除", "delete-modify": "本机删除、对端修改" })[kind] || "文件冲突";
}

function activityRow(item) {
  return `<div class="activity-row"><i class="activity-mark ${escapeHTML(item.level)}"></i><span>${escapeHTML(item.message)}</span><time class="activity-time">${escapeHTML(formatTime(item.time))}</time></div>`;
}

function openModal(html, mode) {
  state.modal = mode;
  $(".modal").classList.toggle("wide-modal", mode === "conflict");
  $("#modal-content").innerHTML = html;
  $("#modal-backdrop").classList.remove("hidden");
}

function closeModal() {
  $("#modal-backdrop").classList.add("hidden");
  state.modal = null;
  state.selectedId = null;
  $(".modal").classList.remove("wide-modal");
}

function openNearby() {
  const nearby = (state.data.nearbyDevices || []).filter(item => !item.paired);
  openModal(`<h2 id="modal-title">添加设备</h2><p class="modal-intro">PolySync 通过 mDNS 自动发现同一局域网中的设备。</p><div class="device-grid modal-device-grid">${nearby.length ? nearby.map(device => peerCard(device, false)).join("") : `<div class="no-activity">暂未发现设备。请确认另一台设备已启动，并允许 mDNS 和 TCP 通过防火墙。</div>`}</div><form id="manual-pair-form"><div class="field"><label for="manual-address">手动地址</label><div class="path-input"><input class="input" id="manual-address" required placeholder="192.168.1.20:45123"><button class="button ghost" type="submit">连接</button></div><small>适用于 mDNS 被防火墙或 VLAN 阻断的网络，设备身份仍会通过证书和验证码确认。</small></div></form><div class="modal-actions"><button class="button primary" data-close>完成</button></div>`, "nearby");
}

async function beginPair(deviceId, address) {
  try {
    const result = await api("/api/pair/start", { method: "POST", body: JSON.stringify({ deviceId, address }) });
    state.pairSession = result.sessionId;
    openModal(`<h2 id="modal-title">输入配对验证码</h2><p class="modal-intro">在另一台设备上允许请求，然后输入它显示的六位数字。</p><form id="pair-form"><div class="field"><input id="pair-code-input" class="input pair-entry" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" required placeholder="000000"></div><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">完成配对</button></div></form>`, "pair");
    $("#pair-code-input").focus();
  } catch (error) { toast(error.message, true); }
}

function openShareInvite() {
  const devices = state.data.pairedDevices || [];
  if (!devices.length) return toast("请先配对一台设备", true);
  openModal(`<h2 id="modal-title">添加同步文件夹</h2><p class="modal-intro">选择已配对设备。对方只需接受邀请并选择本地文件夹。</p><form id="invite-form"><div class="field"><label for="target-device">目标设备</label><select class="select" id="target-device">${devices.map(device => `<option value="${escapeHTML(device.id)}">${escapeHTML(device.name)}${device.online ? " · 在线" : " · 离线"}</option>`).join("")}</select></div><div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" maxlength="80" placeholder="例如：设计资料"></div>${pathField()}<div class="form-row"><label class="check-row"><input id="auto-sync" type="checkbox" checked> 在线时自动同步</label><div class="field"><label for="interval">检查间隔</label><select class="select" id="interval"><option value="30">30 秒</option><option value="60">1 分钟</option><option value="300">5 分钟</option></select></div></div><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">发送邀请</button></div></form>`, "invite");
}

function pathField(value = "") {
  return `<div class="field"><label for="share-path">本地文件夹</label><div class="path-input"><input class="input" id="share-path" required value="${escapeHTML(value)}" placeholder="输入绝对路径"><button class="button ghost browse-button" type="button">浏览…</button></div></div>`;
}

function openAcceptInvitation(id) {
  const invite = state.data.shareInvitations.find(item => item.id === id);
  state.selectedId = id;
  openModal(`<h2 id="modal-title">接受同步邀请</h2><p class="modal-intro">${escapeHTML(invite.deviceName)} 希望同步“${escapeHTML(invite.name)}”。请选择这台设备上的对应文件夹。</p><form id="accept-form">${pathField()}<label class="check-row"><input id="auto-sync" type="checkbox" checked> 在线时自动同步</label><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">接受并同步</button></div></form>`, "accept");
}

async function openConflict(id) {
  try {
    const content = await api(`/api/conflicts/${id}`);
    state.selectedId = id;
    const c = content.conflict;
    const panes = content.text ? `<div class="merge-grid"><div class="merge-pane"><label>共同旧版本</label><textarea class="merge-text" readonly>${escapeHTML(content.base)}</textarea></div><div class="merge-pane"><label>${escapeHTML(c.localDevice)}</label><textarea class="merge-text" readonly>${escapeHTML(content.local)}</textarea></div><div class="merge-pane"><label>${escapeHTML(c.remoteDevice)}</label><textarea class="merge-text" readonly>${escapeHTML(content.remote)}</textarea></div></div><div class="field"><label for="merge-content">合并结果</label><textarea class="merge-text merge-editor" id="merge-content">${escapeHTML(content.merged)}</textarea></div>` : `<div class="warning">这是二进制文件或文件过大，不能在浏览器中进行文本合并。</div>`;
    openModal(`<h2 id="modal-title">解决文件冲突</h2><p class="modal-intro">${escapeHTML(c.path)} · ${escapeHTML(conflictKind(c.kind))}</p>${panes}<div class="choice-actions"><button class="button small resolve-choice" data-choice="local">使用本机版本</button><button class="button small resolve-choice" data-choice="remote">使用对端版本</button><button class="button small danger resolve-choice" data-choice="delete">删除文件</button>${content.text ? `<button class="button primary small resolve-choice" data-choice="merged">保存合并结果</button>` : ""}</div>`, "conflict");
  } catch (error) { toast(error.message, true); }
}

function openEdit(id) {
  const share = state.data.shares.find(item => item.id === id);
  state.selectedId = id;
  openModal(`<h2 id="modal-title">同步设置</h2><p class="modal-intro">修改设置不会移动或删除本地文件。</p><form id="edit-form"><div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" value="${escapeHTML(share.name)}"></div>${pathField(share.path)}<div class="field"><label for="peer-address">缓存的对端地址</label><input class="input" id="peer-address" value="${escapeHTML(share.peerAddress || "")}"></div><div class="form-row"><label class="check-row"><input id="auto-sync" type="checkbox" ${share.autoSync ? "checked" : ""}> 自动同步</label><div class="field"><label for="interval">检查间隔（秒）</label><input class="input" id="interval" type="number" min="5" max="86400" value="${share.intervalSeconds}"></div></div><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存</button></div><div class="danger-zone"><div><strong>移除同步配置</strong><p>不会删除本地文件。</p></div><button type="button" class="button danger small delete-share">移除</button></div></form>`, "edit");
}

function openDeviceSettings() {
  openModal(`<h2 id="modal-title">设备名称</h2><p class="modal-intro">该名称会显示在附近设备、同步记录和冲突界面中。</p><form id="device-form"><div class="field"><label for="device-name">名称</label><input class="input" id="device-name" maxlength="80" required value="${escapeHTML(state.data.device.name)}"></div><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存</button></div></form>`, "device");
}

async function pickFolder(button) {
  button.disabled = true;
  try { $("#share-path").value = (await api("/api/pick-folder", { method: "POST", body: "{}" })).path; }
  catch (error) { toast(error.message, true); }
  finally { button.disabled = false; }
}

document.addEventListener("click", async event => {
  const target = event.target;
  if (target.closest("#add-device")) return openNearby();
  if (target.closest("#add-share") || target.closest("#empty-add")) return openShareInvite();
  if (target.closest(".edit-device")) return openDeviceSettings();
  if (target.closest("#close-modal") || target.matches("[data-close]")) return closeModal();
  if (target.classList.contains("browse-button")) return pickFolder(target);
  const copy = target.closest("[data-copy]");
  if (copy) { await navigator.clipboard.writeText(copy.dataset.copy); return toast("地址已复制"); }
  const pair = target.closest(".pair-device");
  if (pair) return beginPair(pair.dataset.id, pair.dataset.address);
  const approve = target.closest(".approve-pair");
  if (approve) { try { await api(`/api/pair/requests/${approve.dataset.id}/approve`, { method: "POST", body: "{}" }); await refresh(true); } catch (error) { toast(error.message, true); } return; }
  const reject = target.closest(".reject-pair");
  if (reject) { await api(`/api/pair/requests/${reject.dataset.id}`, { method: "DELETE" }); await refresh(true); return; }
  const accept = target.closest(".accept-invite");
  if (accept) return openAcceptInvitation(accept.dataset.id);
  const conflict = target.closest(".resolve-conflict");
  if (conflict) return openConflict(conflict.dataset.id);
  const resolve = target.closest(".resolve-choice");
  if (resolve) {
    if (!confirm("应用此解决方案并同步到另一台设备？")) return;
    try { await api(`/api/conflicts/${state.selectedId}/resolve`, { method: "POST", body: JSON.stringify({ choice: resolve.dataset.choice, content: $("#merge-content")?.value || "" }) }); closeModal(); toast("冲突已解决，正在同步"); await refresh(true); } catch (error) { toast(error.message, true); }
    return;
  }
  const card = target.closest(".share-card");
  if (target.closest(".edit-button") && card) return openEdit(card.dataset.shareId);
  if (target.closest(".sync-button") && card) { try { await api(`/api/shares/${card.dataset.shareId}/sync`, { method: "POST", body: "{}" }); toast("已开始同步"); await refresh(true); } catch (error) { toast(error.message, true); } return; }
  if (target.closest(".delete-share")) {
    const share = state.data.shares.find(item => item.id === state.selectedId);
    if (!confirm(`移除“${share.name}”的同步配置？本地文件不会被删除。`)) return;
    try { await api(`/api/shares/${share.id}`, { method: "DELETE" }); closeModal(); toast("同步配置已移除"); await refresh(); } catch (error) { toast(error.message, true); }
  }
});

document.addEventListener("change", async event => {
  if (!event.target.classList.contains("auto-toggle")) return;
  const share = state.data.shares.find(item => item.id === event.target.closest(".share-card").dataset.shareId);
  try { await api(`/api/shares/${share.id}`, { method: "PUT", body: JSON.stringify({ name: share.name, path: share.path, peerAddress: share.peerAddress || "", autoSync: event.target.checked, intervalSeconds: share.intervalSeconds }) }); toast(event.target.checked ? "已开启自动同步" : "已关闭自动同步"); }
  catch (error) { event.target.checked = !event.target.checked; toast(error.message, true); }
});

document.addEventListener("submit", async event => {
  event.preventDefault();
  const submit = event.target.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    if (event.target.id === "pair-form") {
      await api("/api/pair/confirm", { method: "POST", body: JSON.stringify({ sessionId: state.pairSession, code: $("#pair-code-input").value }) });
      closeModal(); toast("设备配对成功"); await refresh();
    } else if (event.target.id === "manual-pair-form") {
      await beginPair("", $("#manual-address").value.trim());
    } else if (event.target.id === "invite-form") {
      await api("/api/shares/invite", { method: "POST", body: JSON.stringify({ deviceId: $("#target-device").value, name: $("#share-name").value, path: $("#share-path").value, autoSync: $("#auto-sync").checked, intervalSeconds: Number($("#interval").value) }) });
      closeModal(); toast("同步邀请已发送"); await refresh();
    } else if (event.target.id === "accept-form") {
      await api(`/api/share-invitations/${state.selectedId}/accept`, { method: "POST", body: JSON.stringify({ path: $("#share-path").value, autoSync: $("#auto-sync").checked, intervalSeconds: 30 }) });
      closeModal(); toast("邀请已接受，正在首次同步"); await refresh();
    } else if (event.target.id === "edit-form") {
      const share = state.data.shares.find(item => item.id === state.selectedId);
      await api(`/api/shares/${share.id}`, { method: "PUT", body: JSON.stringify({ name: $("#share-name").value, path: $("#share-path").value, peerAddress: $("#peer-address").value, autoSync: $("#auto-sync").checked, intervalSeconds: Number($("#interval").value) }) });
      closeModal(); toast("设置已保存"); await refresh();
    } else if (event.target.id === "device-form") {
      await api("/api/settings", { method: "PUT", body: JSON.stringify({ deviceName: $("#device-name").value }) });
      closeModal(); toast("设备名称已更新，重启后刷新 mDNS 名称"); await refresh();
    }
  } catch (error) { toast(error.message, true); submit.disabled = false; }
});

$("#modal-backdrop").addEventListener("click", event => { if (event.target.id === "modal-backdrop") closeModal(); });
document.addEventListener("keydown", event => { if (event.key === "Escape") closeModal(); });
refresh();
setInterval(() => refresh(true), 2500);
