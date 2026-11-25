const state = { data: null, modal: null, selectedId: null, pairSession: null, modalGeneration: 0, statusRequest: 0, shareLimit: 24, autoSaving: new Map(), toastTimer: null };
const $ = selector => document.querySelector(selector);
const escapeHTML = (value = "") => String(value).replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);


function setContent(selector, html) {
  const node = $(selector);
  if (node._renderedHTML === html) return;
  // Do not interrupt a keyboard edit while a background refresh is in flight.
  if (node.contains(document.activeElement) && document.activeElement.matches("input, select, textarea")) return;
  node.innerHTML = html;
  node._renderedHTML = html;
}

const deviceIllustration = '<svg viewBox="0 0 64 48" fill="none" aria-hidden="true"><circle class="orbit" cx="32" cy="24" r="21" stroke="#aec7ac" stroke-dasharray="3 5"/><rect x="5" y="12" width="28" height="21" rx="6" fill="#d8e6d0" stroke="#698668" stroke-width="1.5"/><path d="M14 38h11m-5-5v5" stroke="#698668" stroke-width="1.5"/><rect x="40" y="16" width="17" height="28" rx="5" fill="#f8faf6" stroke="#698668" stroke-width="1.5"/><path d="M46 39h5" stroke="#698668" stroke-linecap="round"/></svg>';
const folderIllustration = '<svg viewBox="0 0 64 48" fill="none" aria-hidden="true"><rect x="18" y="4" width="32" height="31" rx="8" fill="#dce8d5" transform="rotate(10 34 20)"/><path d="M7 17a6 6 0 0 1 6-6h11l5 6h22a6 6 0 0 1 6 6v15a6 6 0 0 1-6 6H13a6 6 0 0 1-6-6Z" fill="#e7eedc" stroke="#789371" stroke-width="1.5"/><path d="m26 29 5-5 5 5m-5-5v12" stroke="#53764d" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function modalFocusable() {
  return [...$(".modal").querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]')].filter(node => node.getClientRects().length);
}

async function api(path, options = {}) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json", ...(options.headers || {}) }, ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error || `请求失败 (${response.status})`);
    error.details = body;
    error.status = response.status;
    throw error;
  }
  return body;
}

function toast(message, error = false) {
  const node = $("#toast");
  node.textContent = message;
  node.className = `toast show${error ? " error" : ""}`;
  clearTimeout(state.toastTimer);
  state.toastTimer = setTimeout(() => node.className = "toast", 2800);
}

const timeOfDay = new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" });
const monthAndTime = new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });

function formatTime(value) {
  if (!value || value.startsWith("0001-")) return "从未";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知时间";
  return date.toDateString() === new Date().toDateString() ? `今天 ${timeOfDay.format(date)}` : monthAndTime.format(date);
}

async function refresh(silent = false) {
  const request = ++state.statusRequest;
  try {
    const data = await api("/api/status");
    if (request !== state.statusRequest) return;
    state.data = data;
    $(".service-state").classList.remove("offline");
    setContent(".service-state", "<i></i>服务在线");
    render();
  } catch (error) {
    if (request !== state.statusRequest) return;
    $(".service-state").classList.add("offline");
    setContent(".service-state", "<i></i>服务离线");
    if (!silent) toast(error.message, true);
  }
}

function render() {
  if (typeof renderTourInvite === "function") renderTourInvite();
  const { device, shares, activities } = state.data;
  $("#add-share").hidden = !shares.length || $("#sync-workspace").hidden;
  const note = $("#background-note span");
  if (note) note.textContent = state.data.trayEnabled
    ? "关闭页面后继续同步，可从系统托盘打开或退出。"
    : "关闭页面后继续同步。通过终端启动时，按 Ctrl+C 退出。";
  const address = device.addresses.find(item => !item.startsWith("127.")) || device.addresses[0];
  setContent("#device-card", `<div class="device-top"><div><div class="device-label">当前设备</div><div class="device-name">${escapeHTML(device.name)}</div></div><div class="device-tools"><div class="platform">${escapeHTML(device.platform)}</div><button class="text-button edit-device">修改名称</button></div></div><div class="address-line"><span>${escapeHTML(address)}</span><button class="text-button" data-copy="${escapeHTML(address)}">复制地址</button></div>`);
  renderAttention();
  renderDevices();
  $("#share-count").textContent = `${shares.length} 个文件夹`;
  renderShareList(shares);
  $("#activity-section").hidden = !activities.length;
  if (typeof renderTransferDevices === "function" && !$("#transfer-workspace").hidden) renderTransferDevices();
  renderConflicts();
  setContent("#activity-list", activities.length ? activities.slice(0, 10).map(activityRow).join("") : `<div class="no-activity">设备配对或同步开始后，记录会显示在这里。</div>`);
}

function renderAttention() {
  const items = [];
  (state.data.pairingRequests || []).forEach(request => items.push(`<div class="attention-card"><div><strong>${escapeHTML(request.deviceName)} 请求配对</strong><p>${request.code ? "将验证码告诉发起设备，60 秒内有效。" : "确认设备名称无误后允许本次请求。"}</p></div>${request.code ? `<div class="pair-code">${escapeHTML(request.code)}</div>` : `<div class="choice-actions"><button class="button small approve-pair" data-id="${escapeHTML(request.id)}">允许</button><button class="button small danger reject-pair" data-id="${escapeHTML(request.id)}">拒绝</button></div>`}</div>`));
  (state.data.shareInvitations || []).forEach(invite => items.push(`<div class="attention-card"><div><strong>${escapeHTML(invite.deviceName)} 邀请同步“${escapeHTML(invite.name)}”</strong><p>选择一个本地文件夹即可加入，不需要再次验证身份。</p></div><button class="button small accept-invite" data-id="${escapeHTML(invite.id)}">选择文件夹</button></div>`));
  setContent("#attention-list", items.join(""));
}

function renderDevices() {
  const paired = state.data?.pairedDevices || [];
  const nearby = (state.data?.nearbyDevices || []).filter(device => !device.paired);
  const title = paired.length ? "管理设备" : "添加设备";
  $("#add-device").textContent = title;
  $("#device-count").textContent = `${paired.length} 台已配对 · ${nearby.length} 台附近设备`;
  if (state.modal === "unpair") return;

  const cards = [...paired.map(device => peerCard(device, true)), ...nearby.map(device => peerCard(device, false))].join("");
  const focused = document.activeElement;
  const focusedPeer = (focused?.matches(".unpair-device") && paired.some(device => device.id === focused.dataset.id))
    || (focused?.matches(".pair-device") && nearby.some(device => device.id === focused.dataset.id));
  if (!focusedPeer || !$("#device-list").contains(focused)) {
    setContent("#device-list", cards || `<div class="empty"><div><div class="empty-symbol">${deviceIllustration}</div><h3>正在发现附近设备</h3><p>确保两台设备位于同一局域网并已启动 PolySync。</p></div></div>`);
  }
  if (state.modal === "nearby") {
    $("#modal-title").textContent = title;
    if (!focusedPeer || !$("#dialog-device-list").contains(focused)) {
      setContent("#dialog-device-list", cards || `<div class="no-activity">在另一台设备上打开 PolySync，并连接同一局域网。</div>`);
    }
  }
}

function peerCard(device, paired) {
  const online = Boolean(device.online);
  const fingerprint = device.fingerprint || (device.publicKey ? device.publicKey.slice(0, 16) : "");
  return `<article class="peer-card"><div class="peer-top"><div><div class="peer-name">${escapeHTML(device.name)}</div><div class="peer-detail" title="${escapeHTML(fingerprint)}">${escapeHTML(fingerprint)}</div></div><span class="peer-state ${online ? "online" : ""}"><i></i>${online ? "在线" : "离线"}</span></div><div class="peer-footer">${paired ? `<span class="trust-badge">已安全配对</span><button class="button small danger unpair-device" data-id="${escapeHTML(device.id)}" aria-label="取消与 ${escapeHTML(device.name)} 的配对">取消配对</button>` : `<span class="muted">附近设备</span><button class="button small pair-device" data-id="${escapeHTML(device.id)}" data-address="${escapeHTML((device.addresses || [])[0] || "")}">配对</button>`}</div></article>`;
}

function statusText(share) {
  if (share.state === "disconnected") return "文件夹连接已断开 · 本地文件保留";
  if (share.state === "unpaired") return "已取消配对 · 本地文件保留";
  if (share.state === "pending") return "等待对方接受邀请";
  if (share.state === "legacy") return "需要重新配对并邀请";
  const status = share.status || {};
  if (status.state === "syncing") return status.message || "同步中…";
  if (status.state === "error") return status.message || "同步失败";
  if (status.state === "paused") return status.message || "文件夹同步已暂停";
  if (status.state === "waiting") return status.message || "等待对端恢复同步";
  if (hasLastSync(status)) return `${status.message && status.message !== "同步完成" ? status.message : "已同步"} · ${formatTime(status.lastSync)}`;
  return status.message || "等待首次同步";
}

function hasLastSync(status) {
  return Boolean(status?.lastSync && !String(status.lastSync).startsWith("0001-"));
}

function lastSuccessfulSync(status) {
  return (status.state === "paused" || status.state === "waiting") && hasLastSync(status) ? `上次同步成功 · ${formatTime(status.lastSync)}` : "";
}

function shareStatusID(id) {
  return `share-status-${String(id).replace(/[^a-zA-Z0-9_-]/g, "-") || "folder"}`;
}

function syncButtonState(share) {
  const mode = share.status?.state;
  if (share.state !== "active") return { label: "立即同步", disabled: true };
  if (mode === "paused") return { label: "已暂停", paused: true };
  if (mode === "syncing") return { label: "同步中", disabled: true };
  return { label: "立即同步" };
}

function shareCard(share) {
  const status = share.status || { state: "idle" };
  const active = share.state === "active";
  const sync = syncButtonState(share);
  const saving = state.autoSaving.has(share.id);
  const automatic = saving ? state.autoSaving.get(share.id) : share.autoSync;
  const members = share.members || [];
  const names = members.filter(member => member.deviceId !== state.data?.device?.id && member.state !== "unpaired").map(member => memberLabel(member));
  const group = names.length ? `<div class="share-devices">${share.localRole === "follower" ? roleLabel("follower", members[0]?.allowWriteBack) + " · " : ""}${escapeHTML(names.join("、"))}</div>` : "";
  const review = share.localRole === "follower" && members.some(member => member.writeBackPending)
    ? '<button class="button small review-writeback">查看待回传修改</button>' : "";
  const canInvite = (active || share.state === "disconnected") && share.localRole !== "follower";
  const statusID = shareStatusID(share.id);
  const syncAttributes = [
    sync.disabled ? "disabled" : "",
    sync.paused ? 'aria-disabled="true"' : "",
    `aria-describedby="${escapeHTML(statusID)}"`
  ].filter(Boolean).join(" ");

  return `<article class="share-card" data-share-id="${escapeHTML(share.id)}"><div class="share-header"><div class="folder-icon"></div><div class="card-actions"><button class="button small sync-button" ${syncAttributes}>${sync.label}</button>${canInvite ? '<button class="button small invite-member">添加设备</button>' : ""}<button class="icon-button more edit-button" aria-label="${escapeHTML(share.name)}的同步设置">•••</button></div></div><div class="share-name">${escapeHTML(share.name)}</div><div class="share-path" title="${escapeHTML(share.path)}">${escapeHTML(share.path)}</div>${group}${review}<div class="share-meta"><div class="share-feedback"><div class="status ${escapeHTML(status.state || "idle")}" id="${escapeHTML(statusID)}" role="status" aria-live="polite" aria-atomic="true"><i class="status-dot" aria-hidden="true"></i><span>${escapeHTML(statusText(share))}</span></div><small class="last-success"${lastSuccessfulSync(status) ? "" : " hidden"}>${escapeHTML(lastSuccessfulSync(status))}</small></div><label class="auto-control"><span>自动</span><span class="switch"><input class="auto-toggle" type="checkbox" ${automatic ? "checked" : ""} ${!active || saving ? "disabled" : ""}><i class="slider"></i></span></label></div></article>`;
}

function renderShareList(shares) {
  const list = $("#share-list");
  $("#share-more").hidden = shares.length <= state.shareLimit;
  if (!shares.length) {
    setContent("#share-list", emptyShares());
    return;
  }
  if (list._renderedHTML !== null) { list.replaceChildren(); list._renderedHTML = null; }
  const existing = new Map([...list.children].map(card => [card.dataset.shareId, card]));
  shares.slice(0, state.shareLimit).forEach((share, index) => {
    const html = shareCard(share);
    let card = existing.get(share.id);
    existing.delete(share.id);
    if (!card || card._html !== html) {
      const structure = shareCard({ ...share, status: undefined });
      if (card && (card._structure === structure || card.contains(document.activeElement))) {
        // The status region must stay mounted for assistive technology to
        // announce a change. Keep keyboard focus when a folder goes offline.
        const status = card.querySelector(".status");
        const message = statusText(share);
        status.className = "status " + (share.status?.state || "idle");
        if (status.lastElementChild.textContent !== message) status.lastElementChild.textContent = message;
        const previous = card.querySelector(".last-success");
        const recency = lastSuccessfulSync(share.status || {});
        if (previous.textContent !== recency) previous.textContent = recency;
        previous.hidden = !recency;
        const button = card.querySelector(".sync-button");
        const sync = syncButtonState(share);
        if (!button.hasAttribute("data-requesting")) {
          if (button.textContent !== sync.label) button.textContent = sync.label;
          button.disabled = Boolean(sync.disabled);
          if (sync.paused) button.setAttribute("aria-disabled", "true");
          else button.removeAttribute("aria-disabled");
        }
        card._html = card._structure === structure ? html : null;
      } else {
        const template = document.createElement("template");
        template.innerHTML = html;
        const next = template.content.firstElementChild;
        if (card) card.replaceWith(next);
        next._html = html;
        next._structure = structure;
        card = next;
      }
    }
    if (list.children[index] !== card) list.insertBefore(card, list.children[index] || null);
  });
  existing.forEach(card => card.remove());
}

function memberLabel(member) {
  return state.data?.pairedDevices?.find(device => device.id === member.deviceId)?.name || member.deviceName || member.deviceId;
}

function roleLabel(role, writeBack) {
  if (role !== "follower") return "共同编辑";
  return writeBack ? "跟随，可回传修改" : "仅接收更新";
}

function emptyShares() {
  return `<div class="empty"><div><div class="empty-symbol">${folderIllustration}</div><h3>从第一个同步文件夹开始</h3><p>先配对一台设备，然后发送文件夹同步邀请。</p><button class="button primary" id="empty-add">添加同步文件夹</button></div></div>`;
}

function renderConflicts() {
  const conflicts = (state.data.conflicts || []).filter(item => item.status === "pending");
  $("#conflict-section").classList.toggle("hidden", conflicts.length === 0);
  $("#conflict-count").textContent = `${conflicts.length} 个待解决`;
  setContent("#conflict-list", conflicts.map(item => `<div class="conflict-row"><div><div class="conflict-path">${escapeHTML(item.path)}</div><div class="conflict-kind">${escapeHTML(conflictKind(item.kind))} · ${escapeHTML(item.localDevice)} / ${escapeHTML(item.remoteDevice)}</div></div><button class="button small resolve-conflict" data-id="${escapeHTML(item.id)}">查看并合并</button></div>`).join(""));
}

function conflictKind(kind) {
  return ({ "modify-modify": "双方都修改", "add-add": "双方同名新建", "modify-delete": "本机修改、对端删除", "delete-modify": "本机删除、对端修改" })[kind] || "文件冲突";
}

function activityRow(item) {
  return `<div class="activity-row"><i class="activity-mark ${escapeHTML(item.level)}"></i><span>${escapeHTML(item.message)}</span><time class="activity-time">${escapeHTML(formatTime(item.time))}</time></div>`;
}

function openModal(html, mode, initialFocus) {
  if (!state.modal) state.returnFocus = document.activeElement;
  const changing = Boolean(state.modal);
  const generation = ++state.modalGeneration;
  state.modal = mode;
  $(".modal").classList.toggle("wide-modal", mode === "conflict");
  const body = $("#modal-content");
  body.innerHTML = html;
  if (changing && body.animate && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
    body.getAnimations().forEach(animation => animation.cancel());
    body.animate([{opacity:.45}, {opacity:1}], {duration:parseFloat(getComputedStyle(body).getPropertyValue("--feedback")),easing:"ease-out"});
  }
  $("#modal-backdrop").inert = false;
  $("#modal-backdrop").classList.remove("hidden");
  document.body.style.overflow = "hidden";
  $(".shell").inert = true;
  requestAnimationFrame(() => {
    if (generation !== state.modalGeneration) return;
    const content = $("#modal-content");
    const target = (initialFocus && content.querySelector(initialFocus)) || content.querySelector("input, select, textarea, button") || $("#close-modal");
    target?.focus();
  });
}

function closeModal() {
  $("#modal-backdrop").inert = true;
  $("#modal-backdrop").classList.add("hidden");
  state.modalGeneration++;
  state.resumeInvite = null;
  state.modal = null;
  state.selectedId = null;
  state.unpairReturn = null;
  $(".modal").classList.remove("wide-modal");
  document.body.style.overflow = "";
  $(".shell").inert = false;
  if (state.returnFocus?.isConnected) state.returnFocus.focus({ preventScroll: true });
  state.returnFocus = null;
}

function dismissModal() {
  if (state.modal === "unpair" && state.unpairReturn) {
    const previous = state.unpairReturn;
    state.selectedId = null;
    state.unpairReturn = null;
    openNearby(previous);
    return;
  }
  if (state.resumeInvite?.draft && ["nearby","pair"].includes(state.modal)) {
    const resume = state.resumeInvite;
    state.resumeInvite = null;
    openShareInvite(state.data.shares.find(share => share.id === resume.shareID), resume.draft);
    return;
  }
  closeModal();
}

function openNearby(previous) {
  openModal(`<h2 id="modal-title">添加设备</h2><p class="modal-intro">${state.resumeInvite ? "先连接另一台设备，配对后继续添加文件夹。" : "打开另一台设备上的 PolySync，同一局域网中的设备会显示在这里。"}</p><div class="device-grid modal-device-grid" id="dialog-device-list"></div><form id="manual-pair-form"><div class="field"><label for="manual-address">手动地址</label><div class="path-input"><input class="input" id="manual-address" required placeholder="192.168.1.20:45123"><button class="button ghost" type="submit">连接</button></div><small>找不到设备时，在另一台设备的首页复制连接地址。</small></div><p class="form-error" id="manual-error" role="alert"></p></form><div class="modal-actions"><button class="button primary" data-close>完成</button></div>`, "nearby", previous && `.unpair-device[data-id="${CSS.escape(previous.id)}"]`);
  if (previous) $("#manual-address").value = previous.address;
  renderDevices();
}

function openUnpair(id) {
  const device = (state.data.pairedDevices || []).find(item => item.id === id);
  if (!device) return;
  state.unpairReturn = state.modal === "nearby" ? { id, address: $("#manual-address").value } : null;
  const affected = state.data.shares.filter(share => share.state !== "unpaired" && (share.peerDeviceId === id || share.members?.some(member => member.deviceId === id)));
  const count = affected.filter(share => !share.members?.some(member => member.deviceId !== id && member.deviceId !== state.data.device?.id && member.state === "active")).length;
  openModal(`<h2 id="modal-title">取消配对</h2><p class="modal-intro">取消与“${escapeHTML(device.name)}”的配对后，本机将不再接受它的随传或同步。${count ? `关联的 ${count} 个同步文件夹将停用。` : ""}本地文件、工作区和历史记录会保留。</p><p class="modal-intro">此操作仅在本机生效，对方需单独取消配对。之后如需重新连接，请在双方取消配对后重新验证，并创建新的同步邀请。</p><form id="unpair-form"><p id="unpair-error" role="alert"></p><div class="modal-actions"><button class="button ghost" type="button" data-close>保留配对</button><button class="button danger" type="submit">取消配对</button></div></form>`, "unpair");
  state.selectedId = id;
}

async function beginPair(deviceId, address, trigger) {
  const generation = state.modalGeneration;
  const oldText = trigger.textContent;
  trigger.disabled = true;
  trigger.textContent = "正在连接…";
  const problem = state.modal === "nearby" && $("#manual-error");
  if (problem) problem.textContent = "";
  try {
    const result = await api("/api/pair/start", { method: "POST", body: JSON.stringify({ deviceId, address }) });
    if (generation !== state.modalGeneration) return;
    state.pairSession = result.sessionId;
    openModal(`<h2 id="modal-title">输入配对验证码</h2><p class="modal-intro">在另一台设备上允许请求，输入它显示的六位数字。配对后可相互同步文件或随传。</p><form id="pair-form"><div class="field"><label for="pair-code-input">六位验证码</label><input id="pair-code-input" class="input pair-entry" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required placeholder="000000"></div><p class="form-error" role="alert"></p><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">完成配对</button></div></form>`, "pair");
  } catch (error) {
    if (generation !== state.modalGeneration) return;
    if (problem) problem.textContent = error.message;
    else toast(error.message, true);
  } finally {
    trigger.disabled = false;
    trigger.textContent = oldText;
  }
}

function openShareInvite(share, draft, preferred) {
  const hasCollaborator = share?.members?.some(member => member.role === "collaborator" && member.state === "active") || (share && !share.localRole && share.peerDeviceId);
  const collaboratorNote = hasCollaborator
    ? "修改会经本机传递。要在本机关机时继续同步，请在另外两台协作设备间也连接这个文件夹。"
    : "两台设备都可以编辑，修改会相互同步。";
  const memberIDs = new Set([...(share?.members || []).filter(member => member.state !== "unpaired").map(member => member.deviceId), share?.peerDeviceId]);
  const devices = (state.data?.pairedDevices || []).filter(device => !memberIDs.has(device.id));
  if (!devices.length) {
    state.resumeInvite = { shareID: share?.id, known: (state.data?.pairedDevices || []).map(device => device.id) };
    return openNearby();
  }
  const online = devices.filter(device => device.online);
  const previous = preferred || draft?.device;
  const chosen = devices.some(device => device.id === previous) ? previous : online.length === 1 ? online[0].id : devices.length === 1 ? devices[0].id : "";
  const options = devices.map(device => `<option value="${escapeHTML(device.id)}" ${device.id === chosen ? "selected" : ""}>${escapeHTML(device.name)} · ${device.online ? "在线" : "离线"}</option>`).join("");
  const local = share ? "" : `${pathField()}<details class="advanced"><summary>名称与同步频率</summary><div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" maxlength="80" placeholder="默认使用文件夹名称"></div><label class="check-row"><input id="auto-sync" type="checkbox" checked> 在线时自动同步</label><div class="field"><label for="interval">检查间隔</label><select class="select" id="interval"><option value="30">30 秒</option><option value="60">1 分钟</option><option value="300">5 分钟</option></select></div></details>`;
  state.selectedId = share?.id || null;
  openModal(`<h2 id="modal-title">${share ? "为文件夹添加设备" : "添加同步文件夹"}</h2><p class="modal-intro">${share ? escapeHTML(share.name) + " · " + escapeHTML(share.path) : "对方接受邀请并选好本地文件夹后，就会开始同步。"}</p><form id="invite-form">${local}<div class="field"><label for="target-device">邀请设备</label><select class="select" id="target-device" required>${chosen ? "" : '<option value="">选择一台设备</option>'}${options}</select><button class="text-button" type="button" id="invite-pair">连接其他设备</button></div><div class="field"><label for="invite-role">这台设备如何同步</label><select class="select" id="invite-role"><option value="collaborator">共同编辑</option><option value="follower">跟随此文件夹</option></select><small id="role-note" data-collaborator-note="${collaboratorNote}">${collaboratorNote}</small></div><label class="check-row" id="write-back-row" hidden><input type="checkbox" id="allow-write-back"> 允许这台设备回传修改</label><p class="form-error" role="alert"></p><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">发送邀请</button></div></form>`, "invite", share ? "#target-device" : "#share-path");
  if (draft) {
    $("#invite-role").value = draft.role;
    $("#invite-role").dispatchEvent(new Event("change", {bubbles:true}));
    $("#allow-write-back").checked = draft.write;
    if (!share) {
      $("#share-path").value = draft.path;
      $("#share-name").value = draft.name;
      $("#interval").value = draft.interval;
      $("#auto-sync").checked = draft.auto;
      $("#invite-form details").open = draft.advanced;
    }
  }

}

function pathField(value = "") {
  return `<div class="field"><label for="share-path">本地文件夹</label><div class="path-input"><input class="input" id="share-path" required value="${escapeHTML(value)}" placeholder="输入绝对路径"><button class="button ghost browse-button" type="button">浏览…</button></div></div>`;
}

function openAcceptInvitation(id) {
  const invite = state.data.shareInvitations.find(item => item.id === id);
  if (!invite) return;
  const existing = state.data.shares.find(item => item.id === invite.shareId);
  state.selectedId = id;
  openModal(`<h2 id="modal-title">接受同步邀请</h2><p class="modal-intro">${escapeHTML(invite.deviceName)} 希望同步“${escapeHTML(invite.name)}”。${existing ? "将使用本机已有的文件夹。" : "请选择这台设备上的对应文件夹。"}</p><p class="invite-mode">同步方式 · ${escapeHTML(roleLabel(invite.role, invite.allowWriteBack))}</p><form id="accept-form">${existing ? `<p class="share-path">${escapeHTML(existing.path)}</p>` : pathField()}<label class="check-row"><input id="auto-sync" type="checkbox" checked> 在线时自动同步</label><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">接受并同步</button></div></form>`, "accept");
}

async function openConflict(id) {
  openModal('<h2 id="modal-title">解决文件冲突</h2><p id="conflict-loading" role="status">正在读取文件版本…</p>', "conflict");
  const generation = state.modalGeneration;
  try {
    const content = await api(`/api/conflicts/${encodeURIComponent(id)}`);
    if (generation !== state.modalGeneration) return;
    state.selectedId = id;
    const c = content.conflict;
    const pane = (id, label, value) => `<div class="merge-pane"><label for="${id}">${escapeHTML(label)}</label><textarea id="${id}" class="merge-text" readonly>${escapeHTML(value)}</textarea></div>`;
    const base = c.baseHash ? pane("merge-base", "共同旧版本", content.base) : "";
    const panes = content.text ? `${base ? "" : '<p class="modal-intro">没有可核对的共同版本，请比较两台设备的内容。</p>'}
      <div class="merge-grid${base ? "" : " two-versions"}">${base}${pane("merge-local", c.localDevice, content.local)}${pane("merge-remote", c.remoteDevice, content.remote)}</div>
      <div class="field"><label for="merge-content">合并结果</label><textarea class="merge-text merge-editor" id="merge-content">${escapeHTML(content.merged)}</textarea></div>`
      : '<div class="warning">这是二进制文件或文件过大，不能在浏览器中进行文本合并。</div>';
    openModal(`<h2 id="modal-title">解决文件冲突</h2><p class="modal-intro">${escapeHTML(c.path)} · ${escapeHTML(conflictKind(c.kind))}</p>${panes}<div class="choice-actions"><button class="button small resolve-choice" data-choice="local">使用本机版本</button><button class="button small resolve-choice" data-choice="remote">使用对端版本</button><button class="button small danger resolve-choice" data-choice="delete">删除文件</button>${content.text ? `<button class="button primary small resolve-choice" data-choice="merged">保存合并结果</button>` : ""}</div>`, "conflict");
  } catch (error) { if (generation === state.modalGeneration) $("#conflict-loading").textContent = error.message; }
}

async function openWriteBack(share, message = "") {
  const id = share.id;
  openModal(`<h2 id="modal-title">确认回传修改</h2><p class="modal-intro">${escapeHTML(share.name)} · 允许后，本机修改将发送到邀请设备。</p><div id="writeback-preview" role="status">正在比较文件…</div><div class="modal-actions"><button class="button ghost" data-close>稍后处理</button><button class="button primary" id="confirm-writeback" disabled>允许回传</button></div>`, "writeback", "[data-close]");
  const generation = state.modalGeneration;
  state.selectedId = id;
  try {
    const preview = await api(`/api/shares/${encodeURIComponent(id)}/writeback-preview`);
    if (generation !== state.modalGeneration) return;
    const list = preview.paths || [];
    const labels = {added:"新增", modified:"修改", deleted:"删除"};
    $("#writeback-preview").innerHTML = `${message ? `<p class="form-error">${escapeHTML(message)}</p>` : ""}<p>新增 ${escapeHTML(preview.added)} 项 · 修改 ${escapeHTML(preview.modified)} 项 · 删除 ${escapeHTML(preview.deleted)} 项</p>${list.length ? `<ul class="review-files">${list.map(item => `<li><span>${escapeHTML(labels[item.change] || item.change)}</span> ${escapeHTML(item.path)}</li>`).join("")}</ul>` : ""}${list.length < preview.added + preview.modified + preview.deleted ? "<p>仅显示前 50 项。</p>" : ""}`;
    state.writeBackToken = preview.token;
    $("#confirm-writeback").disabled = false;
  } catch (error) {
    if (generation !== state.modalGeneration) return;
    $("#writeback-preview").textContent = error.message;
  }
}

async function confirmWriteBack(button) {
  const generation = state.modalGeneration;
  const share = state.data.shares.find(item => item.id === state.selectedId);
  if (!share) return;
  button.disabled = true;
  try {
    await api(`/api/shares/${encodeURIComponent(share.id)}/writeback-confirm`, {method:"POST", body:JSON.stringify({token:state.writeBackToken})});
    if (generation === state.modalGeneration) closeModal();
    await refresh(true);
    toast("已允许回传本机修改");
  } catch (error) {
    if (generation !== state.modalGeneration) return;
    if (error.status === 409) return openWriteBack(share, error.message);
    $("#writeback-preview").textContent = error.message;
    button.disabled = false;
  }
}

function memberRow(share, member) {
  const name = escapeHTML(memberLabel(member));
  const managesFollower = share.localRole === "collaborator" && member.role === "follower" && member.state === "active";
  const retry = member.state === "pending" && share.localRole === "collaborator"
    ? `<button class="button small retry-invite" type="button" data-id="${escapeHTML(member.deviceId)}">重发邀请</button>` : "";
  const rights = managesFollower
    ? `<button class="button ghost small" type="button" data-member-action="write">${member.allowWriteBack ? "关闭回传" : "允许回传"}</button>` : "";
  const mode = roleLabel(share.localRole === "follower" ? "follower" : member.role, member.allowWriteBack);
  return `<details class="member-row" data-member-id="${escapeHTML(member.deviceId)}"><summary>${name}<small>${mode}${member.state === "pending" ? " · 等待接受" : ""}</small></summary><div class="member-actions">${rights}${retry}<button type="button" class="button ghost small" data-member-action="remove">断开此连接</button></div><div class="member-confirm" hidden><p></p><button class="button ghost small" type="button" data-member-action="cancel">取消</button> <button class="button small" type="button" data-member-action="confirm"></button></div><p class="form-error" role="alert"></p></details>`;
}

async function actOnMember(button) {
  const row = button.closest(".member-row");
  const group = state.data.shares.find(share => share.id === state.selectedId);
  const member = group?.members?.find(item => item.deviceId === row.dataset.memberId);
  if (!member) return;
  const confirm = row.querySelector(".member-confirm");
  const actions = row.querySelector(".member-actions");
  const error = row.querySelector(".form-error");
  const action = button.dataset.memberAction;
  if (action === "cancel") {
    confirm.hidden = true;
    actions.hidden = false;
    error.textContent = "";
    actions.querySelector(`[data-member-action="${row.dataset.intent}"]`)?.focus();
    return;
  }
  if (action !== "confirm") {
    row.dataset.intent = action;
    row.dataset.write = String(!member.allowWriteBack);
    actions.hidden = true;
    confirm.hidden = false;
    const remove = action === "remove", enable = !member.allowWriteBack;
    confirm.querySelector("p").textContent = remove
      ? `断开与“${memberLabel(member)}”的此文件夹连接？本地文件、设备配对及其他设备的连接都会保留。`
      : enable ? "允许这台设备回传修改？已有的本地改动需要对方确认后才能发送。" : "关闭回传后，这台设备只接收更新。本地文件将保留。";
    confirm.querySelector('[data-member-action="confirm"]').textContent = remove ? "断开连接" : enable ? "允许回传" : "关闭回传";
    confirm.querySelector('[data-member-action="cancel"]').focus();
    return;
  }
  const remove = row.dataset.intent === "remove";
  button.disabled = true;
  const cancel = confirm.querySelector('[data-member-action="cancel"]');
  cancel.disabled = true;
  error.textContent = "";
  const url = `/api/shares/${encodeURIComponent(group.id)}/members/${encodeURIComponent(member.deviceId)}`;
  try {
    await api(url, remove ? {method:"DELETE"} : {method:"PUT", body:JSON.stringify({allowWriteBack:row.dataset.write === "true"})});
    await refresh(true);
    toast(remove ? "已断开文件夹连接，本地文件保留" : "回传权限已更新");
    if (!row.isConnected) return;
    const newGroup = state.data.shares.find(share => share.id === group.id);
    const updated = newGroup?.members?.find(item => item.deviceId === member.deviceId);
    if (updated && !remove) {
      const template = document.createElement("template");
      template.innerHTML = memberRow(newGroup, updated);
      const replacement = template.content.firstElementChild;
      row.replaceWith(replacement);
      replacement.querySelector("summary").focus();
    } else {
      const roster = row.parentElement;
      row.remove();
      if (!roster.querySelector(".member-row")) roster.remove();
      ($("#edit-form .member-row summary") || $("#share-name")).focus();
    }
  } catch (problem) {
    error.textContent = problem.message;
    button.disabled = false;
    cancel.disabled = false;
    button.focus();
  }
}

function openEdit(id) {
  const share = state.data.shares.find(item => item.id === id);
  state.selectedId = id;
  const members = share.members || [];
  const roster = members.length ? `<section class="member-list"><h3>同步设备</h3>${members.map(member => memberRow(share, member)).join("")}</section>` : "";
  openModal(`<h2 id="modal-title">同步设置</h2><p class="modal-intro">修改设置不会移动或删除本地文件。</p><form id="edit-form">${roster}<div class="field"><label for="share-name">显示名称</label><input class="input" id="share-name" value="${escapeHTML(share.name)}"></div>${pathField(share.path)}<details class="advanced"><summary>连接与检查间隔</summary><div class="field"><label for="peer-address">连接地址</label><input class="input" id="peer-address" value="${escapeHTML(share.peerAddress || "")}"></div><div class="form-row"><label class="check-row"><input id="auto-sync" type="checkbox" ${share.autoSync ? "checked" : ""}> 自动同步</label><div class="field"><label for="interval">检查间隔（秒）</label><input class="input" id="interval" type="number" min="5" max="86400" value="${share.intervalSeconds}"></div></div></details><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存</button></div><div class="danger-zone"><div><strong>移除同步配置</strong><p>不会删除本地文件。</p></div><button type="button" class="button danger small delete-share">移除</button></div></form>`, "edit");
}

function openDeviceSettings() {
  openModal(`<h2 id="modal-title">设备名称</h2><p class="modal-intro">该名称会显示在附近设备、同步记录和冲突界面中。</p><form id="device-form"><div class="field"><label for="device-name">名称</label><input class="input" id="device-name" maxlength="80" required value="${escapeHTML(state.data.device.name)}"></div><div class="modal-actions"><button class="button ghost" type="button" data-close>取消</button><button class="button primary" type="submit">保存</button></div></form>`, "device");
}

async function pickFolder(button) {
  const input = $("#share-path");
  button.disabled = true;
  try {
    const result = await api("/api/pick-folder", { method: "POST", body: "{}" });
    if (input.isConnected && result.path) input.value = result.path;
  }
  catch (error) { toast(error.message, true); }
  finally { button.disabled = false; }
}

document.addEventListener("click", async event => {
  const unpair = event.target.closest(".unpair-device");
  if (unpair) return openUnpair(unpair.dataset.id);
  const target = event.target;
  if (target.closest("#share-more")) {
    const first = state.data.shares[state.shareLimit];
    state.shareLimit += 24;
    renderShareList(state.data.shares);
    if (first) $(`[data-share-id="${CSS.escape(first.id)}"] .edit-button`)?.focus();
    return;
  }
  if (target.closest("#confirm-writeback")) return confirmWriteBack(target.closest("#confirm-writeback"));
  if (target.closest(".review-writeback")) {
    const share = state.data.shares.find(item => item.id === target.closest(".share-card").dataset.shareId);
    return openWriteBack(share);
  }
  const memberAction = target.closest("[data-member-action]");
  if (memberAction) return actOnMember(memberAction);
  const retry = target.closest(".retry-invite");
  if (retry) {
    const share = state.data.shares.find(item => item.id === state.selectedId);
    const member = share?.members?.find(item => item.deviceId === retry.dataset.id && item.state === "pending");
    if (!member) return;
    retry.disabled = true;
    retry.textContent = "正在发送…";
    try {
      await api(`/api/shares/${encodeURIComponent(share.id)}/members`, {method:"POST", body:JSON.stringify({deviceId:member.deviceId, role:member.role, allowWriteBack:member.allowWriteBack})});
      toast("邀请已重新发送");
    } catch (error) { toast(error.message, true); }
    finally { retry.disabled = false; retry.textContent = "重发邀请"; await refresh(true); }
    return;
  }
  if (target.closest("#invite-pair")) {
    state.resumeInvite = { shareID:state.selectedId, known:state.data.pairedDevices.map(device => device.id), draft:{
      device:$("#target-device").value, role:$("#invite-role").value, write:$("#allow-write-back").checked,
      path:$("#share-path")?.value, name:$("#share-name")?.value, interval:$("#interval")?.value,
      auto:$("#auto-sync")?.checked, advanced:$("#invite-form details")?.open
    }};
    return openNearby();
  }
  if (target.closest("#add-device")) return openNearby();
  if (target.closest("#add-share") || target.closest("#empty-add")) return openShareInvite();
  if (target.closest(".edit-device")) return openDeviceSettings();
  if (target.closest("#close-modal") || target.matches("[data-close]")) return dismissModal();
  if (target.classList.contains("browse-button")) return pickFolder(target);
  const copy = target.closest("[data-copy]");
  if (copy) {
    try { await navigator.clipboard.writeText(copy.dataset.copy); toast("地址已复制"); }
    catch { toast("无法访问剪贴板，可选中地址手动复制。", true); }
    return;
  }
  const pair = target.closest(".pair-device");
  if (pair) return beginPair(pair.dataset.id, pair.dataset.address, pair);
  const approve = target.closest(".approve-pair");
  if (approve) { try { await api(`/api/pair/requests/${approve.dataset.id}/approve`, { method: "POST", body: "{}" }); await refresh(true); } catch (error) { toast(error.message, true); } return; }
  const reject = target.closest(".reject-pair");
  if (reject) {
    reject.disabled = true;
    try { await api(`/api/pair/requests/${reject.dataset.id}`, { method: "DELETE" }); await refresh(true); }
    catch (error) { toast(error.message, true); }
    finally { reject.disabled = false; }
    return;
  }
  const accept = target.closest(".accept-invite");
  if (accept) return openAcceptInvitation(accept.dataset.id);
  const conflict = target.closest(".resolve-conflict");
  if (conflict) return openConflict(conflict.dataset.id);
  const resolve = target.closest(".resolve-choice");
  if (resolve) {
    if (resolve.dataset.choice === "delete" && !confirm("删除这份文件并把删除操作同步到关联设备？")) return;
    if (["local", "remote"].includes(resolve.dataset.choice) && !confirm("使用选中的版本替换冲突文件，并同步到关联设备？")) return;
    const generation = state.modalGeneration;
    resolve.disabled = true;
    try { await api(`/api/conflicts/${state.selectedId}/resolve`, { method: "POST", body: JSON.stringify({ choice: resolve.dataset.choice, content: $("#merge-content")?.value || "" }) }); if (generation === state.modalGeneration) closeModal(); toast("冲突已解决，正在同步"); await refresh(true); } catch (error) { resolve.disabled = false; toast(error.message, true); }
    return;
  }
  const card = target.closest(".share-card");
  if (target.closest(".invite-member") && card) return openShareInvite(state.data.shares.find(share => share.id === card.dataset.shareId));
  if (target.closest(".edit-button") && card) return openEdit(card.dataset.shareId);
  const sync = target.closest(".sync-button");
  if (sync && card) {
    if (sync.disabled || sync.getAttribute("aria-disabled") === "true") return;
    sync.setAttribute("data-requesting", "");
    sync.disabled = true;
    sync.textContent = "正在请求…";
    try {
      await api(`/api/shares/${encodeURIComponent(card.dataset.shareId)}/sync`, { method: "POST", body: "{}" });
      toast("已请求同步");
    } catch (error) { toast(error.message, true); }
    finally {
      sync.removeAttribute("data-requesting");
      sync.disabled = false;
      sync.textContent = "立即同步";
      await refresh(true);
    }
    return;
  }
  if (target.closest(".delete-share")) {
    const share = state.data.shares.find(item => item.id === state.selectedId);
    if (!confirm(`移除“${share.name}”的同步配置？本地文件不会被删除。`)) return;
    try { await api(`/api/shares/${share.id}`, { method: "DELETE" }); closeModal(); toast("同步配置已移除"); await refresh(); } catch (error) { toast(error.message, true); }
  }
});

document.addEventListener("change", async event => {
  if (event.target.id === "invite-role") {
    const follow = event.target.value === "follower";
    $("#write-back-row").hidden = !follow;
    const note = $("#role-note");
    note.textContent = follow ? "对方从本机接收更新。在对方设备上编辑的文件，默认只留在对方。" : note.dataset.collaboratorNote;
    return;
  }
  const toggle = event.target;
  if (!toggle.classList.contains("auto-toggle")) return;
  const share = state.data.shares.find(item => item.id === toggle.closest(".share-card").dataset.shareId);
  if (!share || state.autoSaving.has(share.id)) return;
  const checked = toggle.checked;
  state.autoSaving.set(share.id, checked);
  toggle.disabled = true;
  try {
    await api(`/api/shares/${encodeURIComponent(share.id)}`, { method:"PUT", body:JSON.stringify({name:share.name, path:share.path, peerAddress:share.peerAddress || "", autoSync:checked, intervalSeconds:share.intervalSeconds})});
    share.autoSync = checked;
    toast(checked ? "已开启自动同步" : "已关闭自动同步");
  } catch (error) {
    toggle.checked = !checked;
    toast(error.message, true);
  } finally {
    state.autoSaving.delete(share.id);
    toggle.disabled = false;
    await refresh(true);
  }
});

document.addEventListener("submit", async event => {
  if (!["manual-pair-form", "pair-form", "unpair-form", "invite-form", "accept-form", "edit-form", "device-form"].includes(event.target.id)) return;
  event.preventDefault();
  const form = event.target;
  const generation = state.modalGeneration;
  const submit = form.querySelector("button[type=submit]");
  if (form.id === "manual-pair-form") return beginPair("", $("#manual-address").value.trim(), submit);
  submit.disabled = true;
  const idleText = submit.textContent;
  submit.textContent = form.id === "invite-form" ? "正在发送…" : "处理中…";
  const finish = () => { if (generation === state.modalGeneration) closeModal(); };
  try {
    if (event.target.id === "unpair-form") {
      $("#unpair-error").textContent = "";
      await api(`/api/paired-devices/${encodeURIComponent(state.selectedId)}`, { method: "DELETE" });
      finish(); toast("已取消配对，本地文件保留"); await refresh();
      if (!state.modal) $("#add-device").focus({ preventScroll: true });
    } else if (event.target.id === "pair-form") {
      await api("/api/pair/confirm", { method: "POST", body: JSON.stringify({ sessionId: state.pairSession, code: $("#pair-code-input").value }) });
      await refresh();
      if (generation !== state.modalGeneration) return;
      const resume = state.resumeInvite;
      state.resumeInvite = null;
      if (resume && state.data?.pairedDevices?.length) {
        const added = state.data.pairedDevices.filter(device => !resume.known.includes(device.id));
        openShareInvite(state.data.shares.find(item => item.id === resume.shareID), resume.draft, added.length === 1 ? added[0].id : undefined);
      } else finish();
      toast("设备配对成功");
    } else if (event.target.id === "invite-form") {
      const role = $("#invite-role").value;
      const invitation = { deviceId: $("#target-device").value, role, allowWriteBack: role === "follower" && $("#allow-write-back").checked };
      const shareID = state.selectedId;
      if (!shareID) Object.assign(invitation, { name: $("#share-name").value, path: $("#share-path").value, autoSync: $("#auto-sync").checked, intervalSeconds: Number($("#interval").value) });
      await api(shareID ? `/api/shares/${encodeURIComponent(shareID)}/members` : "/api/shares/invite", { method: "POST", body: JSON.stringify(invitation) });
      finish(); toast("同步邀请已发送"); await refresh();
    } else if (event.target.id === "accept-form") {
      await api(`/api/share-invitations/${state.selectedId}/accept`, { method: "POST", body: JSON.stringify({ path: $("#share-path")?.value || "", autoSync: $("#auto-sync").checked, intervalSeconds: 30 }) });
      finish(); toast("已接受邀请并连接文件夹"); await refresh();
    } else if (event.target.id === "edit-form") {
      const share = state.data.shares.find(item => item.id === state.selectedId);
      await api(`/api/shares/${share.id}`, { method: "PUT", body: JSON.stringify({ name: $("#share-name").value, path: $("#share-path").value, peerAddress: $("#peer-address").value, autoSync: $("#auto-sync").checked, intervalSeconds: Number($("#interval").value) }) });
      finish(); toast("设置已保存"); await refresh();
    } else if (event.target.id === "device-form") {
      await api("/api/settings", { method: "PUT", body: JSON.stringify({ deviceName: $("#device-name").value }) });
      finish(); toast("设备名称已更新，重启后刷新 mDNS 名称"); await refresh();
    }
  } catch (error) {
    if (form.id === "invite-form") {
      await refresh(true);
      if (generation === state.modalGeneration && error.details?.id) {
        state.selectedId = error.details.id;
        submit.dataset.retry = "true";
        // This invitation already exists on disk. Retries must keep the same grant.
        for (const control of form.querySelectorAll("input, select, .browse-button, #invite-pair")) control.disabled = true;
      }
    }
    if (generation !== state.modalGeneration) return;
    submit.disabled = false;
    submit.textContent = submit.dataset?.retry ? "重试发送" : idleText;
    if (event.target.id === "unpair-form" && state.modal === "unpair") {
      $("#unpair-error").textContent = error.message;
      if (document.activeElement === document.body) submit.focus();
    } else {
      const problem = form.querySelector(".form-error");
      if (problem) problem.textContent = error.message;
      else toast(error.message, true);
    }
  }
});

$("#modal-backdrop").addEventListener("click", event => { if (event.target.id === "modal-backdrop") dismissModal(); });
document.addEventListener("keydown", event => {
  if (!state.modal) return;
  if (event.key === "Escape") { event.preventDefault(); dismissModal(); return; }
  if (event.key !== "Tab") return;
  const elements = modalFocusable();
  const first = elements[0], last = elements[elements.length - 1];
  if (!first) return;
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
});
refresh();
setInterval(() => { if (!document.hidden) refresh(true); }, 2500);
document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(true); });
