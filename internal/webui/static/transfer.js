/* Temporary delivery is independent of synchronization folders. */
const transferUI = { tasks: [], settings: null, files: [], uploading: false, taskID: null, loading: false };
const transferStates = { preparing: "正在准备", connecting: "正在连接", sending: "正在发送", receiving: "正在接收", verifying: "正在确认", complete: "已完成", failed: "失败", cancelled: "已取消", interrupted: "已中断" };
function transferIsActive(value) { return ["preparing", "connecting", "sending", "receiving", "verifying"].includes(value); }
function transferBytes(value) {
  if (!Number.isFinite(value) || value < 0) return "—";
  if (value < 1024) return value + " B";
  const units = ["KiB", "MiB", "GiB"]; let i = -1;
  do { value /= 1024; i++; } while (value >= 1024 && i < 2);
  return value.toFixed(value < 10 ? 1 : 0) + " " + units[i];
}
function transferSelectionError(files) {
  if (!files.length) return "请先选择文件";
  if (files.length > 128) return "一次最多发送 128 个文件";
  if (files.reduce((sum, file) => sum + file.size, 0) > 16 * 1024 ** 3) return "每批文件总量不能超过 16 GiB";
  return "";
}
function setWorkspace(view) {
  const transfer = view === "transfer";
  $("#sync-workspace").hidden = transfer;
  $("#transfer-workspace").hidden = !transfer;
  document.querySelectorAll("[data-workspace]").forEach(button => button.setAttribute("aria-current", String(button.dataset.workspace === view)));
}
function renderTransferDevices() {
  const select = $("#transfer-peer");
  const selected = select.value;
  const peers = state.data?.pairedDevices || [];
  const html = '<option value="">选择接收设备</option>' + peers.map(peer => `<option value="${escapeHTML(peer.id)}" ${peer.online ? "" : "disabled"}>${escapeHTML(peer.name)} · ${peer.online ? "在线" : "离线"}</option>`).join("");
  if (select._options !== html && document.activeElement !== select) {
    select.innerHTML = html; select._options = html; select.value = selected;
  }
  $("#transfer-peer-note").textContent = peers.length ? "已配对设备会自动接收。对方关闭接收或离线时，发送会失败。" : "先在「添加设备」中配对，两台设备需运行支持随传的版本。";
  updateTransferSelection();
}
function updateTransferSelection() {
  setContent("#transfer-selection", transferUI.files.length ? transferUI.files.map((file, i) => `<li><span>${escapeHTML(file.name)}</span><small>${transferBytes(file.size)}</small><button type="button" class="icon-button" data-remove-file="${i}" aria-label="移除 ${escapeHTML(file.name)}" ${transferUI.uploading ? "disabled" : ""}>×</button></li>`).join("") : "");
  $("#transfer-send").disabled = transferUI.uploading || !transferUI.files.length || !$("#transfer-peer").value;
  $("#transfer-files").disabled = transferUI.uploading;
  $("#transfer-peer").disabled = transferUI.uploading;
  $("#transfer-upload-cancel").hidden = !transferUI.uploading;
}
function chooseTransferFiles(files) {
  if (transferUI.uploading) return;
  const selection = [...transferUI.files, ...Array.from(files)];
  const error = transferSelectionError(selection);
  if (error) { $("#transfer-files").value = ""; return toast(error, true); }
  transferUI.files = selection; $("#transfer-files").value = ""; updateTransferSelection();
}
function transferCard(task) {
  const active = transferIsActive(task.state);
  const percent = task.total ? Math.min(100, Math.round(task.done / task.total * 100)) : (task.state === "complete" ? 100 : 0);
  return `<article class="transfer-task" id="transfer-task-${escapeHTML(task.id)}" data-transfer-id="${escapeHTML(task.id)}" tabindex="-1">
    <div class="transfer-task-head"><div><span class="eyebrow">${task.direction === "receive" ? "来自" : "发往"} ${escapeHTML(task.peerName)}</span><h3>${task.files.length === 1 ? escapeHTML(task.files[0].name) : task.files.length + " 个文件"}</h3></div><span class="transfer-badge ${task.state === "failed" ? "error" : ""}">${transferStates[task.state] || "未知状态"}</span></div>
    <p class="muted">${escapeHTML(formatTime(task.created))} · ${transferBytes(task.total)}</p>
    ${active ? `<progress aria-label="传输进度" value="${percent}" max="100"></progress><p class="muted">${transferStates[task.state]} · ${percent}%</p>` : ""}
    ${task.message && task.state !== "complete" ? `<p class="transfer-message">${escapeHTML(task.message)}</p>` : ""}
    <ul class="received-files">${task.files.map((file, index) => `<li><div><strong>${escapeHTML(file.name)}</strong><small>${transferBytes(file.size)}${file.state === "missing" ? " · 暂存文件已不存在" : file.state === "complete" ? " · 已校验" : ""}</small></div>${task.direction === "receive" ? `<div class="transfer-file-actions"><button class="button small" data-transfer-action="save" data-index="${index}" ${file.state === "complete" ? "" : "disabled"}>另存为</button><button class="button ghost small" data-transfer-action="open" data-index="${index}" ${file.state === "complete" ? "" : "disabled"}>打开位置</button></div>` : ""}</li>`).join("")}</ul>
    <div class="transfer-task-foot"><span class="transfer-path" title="${escapeHTML(task.storageDir)}">${escapeHTML(task.storageDir)}</span><div>${active ? '<button class="button ghost small" data-transfer-action="cancel">取消传输</button>' : `${task.direction === "send" ? '<button class="button ghost small" data-transfer-action="retry">重新发送</button>' : ""}<button class="button ghost small" data-transfer-action="remove">移除记录</button>`}</div></div>
  </article>`;
}
function renderTransferTasks() {
  const list = $("#transfer-list");
  // Update cards separately and keep the focused card intact across polling.
  const ids = new Set(transferUI.tasks.map(task => task.id));
  list.querySelectorAll("[data-transfer-id]").forEach(node => { if (!ids.has(node.dataset.transferId) && !node.contains(document.activeElement)) node.remove(); });
  if (!transferUI.tasks.length) {
    if (!list.querySelector(".transfer-empty")) list.innerHTML = '<div class="transfer-empty"><div class="transfer-orbit" aria-hidden="true">↘</div><h3>文件到了，就在这里。</h3><p>接收的文件会自动暂存，你可以随时另存到需要的位置。</p></div>';
    return;
  }
  list.querySelector(".transfer-empty")?.remove();
  transferUI.tasks.forEach((task, i) => {
    let node = document.getElementById("transfer-task-" + task.id);
    const html = transferCard(task);
    if (!node) {
      const template = document.createElement("template"); template.innerHTML = html;
      node = template.content.firstElementChild; node._html = html;
      list.insertBefore(node, list.children[i] || null);
    } else if (node._html !== html && !node.contains(document.activeElement)) {
      const template = document.createElement("template"); template.innerHTML = html;
      const replacement = template.content.firstElementChild; replacement._html = html; node.replaceWith(replacement);
    }
  });
}
async function refreshTransfers() {
  if (transferUI.loading) return;
  transferUI.loading = true;
  try {
    const data = await api("/api/transfers");
    transferUI.tasks = data.tasks || []; transferUI.settings = data.settings;
    $("#transfer-receive-status").textContent = data.settings.autoReceive ? "自动接收已配对设备的文件" : "随传接收已关闭";
    $("#transfer-directory-note").textContent = "暂存于 " + data.settings.directory;
    $("#transfer-connection-error").hidden = true;
    renderTransferTasks();
  } catch (error) {
    $("#transfer-connection-error").hidden = false;
    $("#transfer-connection-error").textContent = "无法读取随传状态：" + error.message;
  } finally { transferUI.loading = false; }
}
function uploadTransfer(id, files) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const form = new FormData(); files.forEach(file => form.append("file", file, file.name));
    xhr.open("PUT", "/api/transfers/" + encodeURIComponent(id) + "/upload");
    xhr.upload.onprogress = event => { if (event.lengthComputable) $("#transfer-upload-note").textContent = "正在交给后台 · " + Math.round(event.loaded / event.total * 100) + "%，请保持页面打开"; };
    xhr.onload = () => {
      let response = {}; try { response = JSON.parse(xhr.responseText); } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(); else reject(new Error(response.error || "上传失败"));
    };
    xhr.onerror = () => reject(new Error("上传连接中断，请重新选择文件"));
    xhr.onabort = () => reject(new Error("上传已取消"));
    transferUI.xhr = xhr; xhr.send(form);
  });
}
async function sendTransfer() {
  if (transferUI.uploading) return;
  const error = transferSelectionError(transferUI.files);
  if (error || !$("#transfer-peer").value) return toast(error || "请选择在线设备", true);
  const files = [...transferUI.files];
  transferUI.uploading = true; updateTransferSelection();
  $("#transfer-upload-note").textContent = "正在创建任务…";
  try {
    const task = await api("/api/transfers", { method: "POST", body: JSON.stringify({ peerId: $("#transfer-peer").value, files: files.map(file => ({ name: file.name, size: file.size })) }) });
    transferUI.taskID = task.id;
    await uploadTransfer(task.id, files);
    transferUI.files = []; $("#transfer-files").value = "";
    $("#transfer-upload-note").textContent = "已交给后台，现在可以关闭页面。";
    toast("已开始发送，可在下方查看结果");
  } catch (error) { $("#transfer-upload-note").textContent = error.message; toast(error.message, true); }
  finally { transferUI.uploading = false; transferUI.taskID = null; transferUI.xhr = null; updateTransferSelection(); await refreshTransfers(); }
}
function showTransferSettings() {
  const settings = transferUI.settings;
  if (!settings) return toast("随传设置尚未加载，请稍后重试", true);
  openModal(`<h2 id="modal-title">随传设置</h2><p class="modal-intro">让临时文件，有自己的落脚处。</p><form id="transfer-settings-form"><label class="check-row"><input id="transfer-auto" type="checkbox" ${settings.autoReceive ? "checked" : ""}> 自动接收已配对设备的文件</label><p class="muted">关闭后拒绝新传输；已经开始的任务继续进行。</p><div class="field"><label for="transfer-directory">接收暂存位置</label><div class="path-input"><input class="input" id="transfer-directory" required value="${escapeHTML(settings.directory)}"><button type="button" class="button ghost" id="transfer-browse">浏览</button></div><small>新位置只用于之后的接收。已有文件留在原处，不会自动移动或清理。暂存位置不能与同步文件夹重叠。</small></div><div class="modal-actions"><button type="button" class="button ghost" data-close>取消</button><button type="submit" class="button primary">保存设置</button></div></form>`, "transfer-settings");
}
function showTransferRemoval(task) {
  openModal(`<h2 id="modal-title">移除这条记录？</h2><p class="modal-intro">默认保留暂存文件。记录移除后，可通过下面的路径找到原件。</p><p class="transfer-message">${escapeHTML(task.storageDir)}</p><form id="transfer-remove-form" data-task="${escapeHTML(task.id)}"><label class="check-row"><input type="checkbox" id="transfer-delete-files"> 同时删除本任务的暂存文件（无法撤销）</label><p class="muted">另存到其他位置的副本不受影响。</p><div class="modal-actions"><button type="button" class="button ghost" data-close>取消</button><button type="submit" class="button danger">移除记录</button></div></form>`, "transfer-remove");
}
async function routeWorkspace() {
  const hash = location.hash;
  setWorkspace(hash.startsWith("#transfer") ? "transfer" : "sync");
  if (hash.startsWith("#transfer")) {
    await refreshTransfers();
    if (hash === "#transfer/send") $("#transfer-files").focus();
    if (hash === "#transfer/settings") showTransferSettings();
    if (hash.startsWith("#transfer/task/")) {
      const id = hash.slice("#transfer/task/".length);
      const node = document.getElementById("transfer-task-" + id);
      if (node) { node.scrollIntoView({ block: "center" }); node.focus(); }
      else toast("记录已移除或暂不可用", true);
    }
  }
  if (hash === "#tour" && typeof startTour === "function") startTour();
}
if (typeof module !== "undefined") module.exports = { transferBytes, transferSelectionError, transferIsActive };
if (typeof document !== "undefined") {
  document.addEventListener("click", async event => {
    const button = event.target.closest("button");
    if (!button) return;
    if (button.dataset.workspace) { location.hash = button.dataset.workspace === "transfer" ? "transfer" : "sync"; setWorkspace(button.dataset.workspace); return; }
    if (button.id === "transfer-settings") return showTransferSettings();
    if (button.id === "transfer-send") return sendTransfer();
    if (button.dataset.removeFile !== undefined) { transferUI.files.splice(Number(button.dataset.removeFile), 1); updateTransferSelection(); return; }
    if (button.id === "transfer-upload-cancel") {
      if (!transferUI.taskID) return toast("任务正在创建，请稍候");
      transferUI.xhr?.abort();
      try { await api("/api/transfers/" + encodeURIComponent(transferUI.taskID) + "/cancel", { method: "POST", body: "{}" }); } catch (error) { toast(error.message, true); }
      return;
    }
    if (button.id === "transfer-browse") {
      button.disabled = true;
      try { const result = await api("/api/pick-folder", { method: "POST", body: "{}" }); if (result.path && $("#transfer-directory")) $("#transfer-directory").value = result.path; }
      catch (error) { toast(error.message, true); } finally { button.disabled = false; } return;
    }
    const action = button.dataset.transferAction;
    if (!action) return;
    const task = transferUI.tasks.find(item => item.id === button.closest("[data-transfer-id]").dataset.transferId);
    if (!task) return;
    if (action === "remove") return showTransferRemoval(task);
    button.disabled = true;
    try {
      const url = "/api/transfers/" + encodeURIComponent(task.id) + (["save", "open"].includes(action) ? "/files/" + button.dataset.index + "/" + action : "/" + action);
      const result = await api(url, { method: "POST", body: "{}" });
      if (action === "save") toast(result.cancelled ? "已取消另存" : "已另存到 " + result.path);
      else if (action === "retry") toast("已创建新的发送任务");
      else if (action === "cancel") toast("正在取消传输");
      await refreshTransfers();
    } catch (error) { toast(error.message, true); } finally { button.disabled = false; }
  });
  document.addEventListener("submit", async event => {
    if (!["transfer-settings-form", "transfer-remove-form"].includes(event.target.id)) return;
    event.preventDefault();
    const form = event.target, button = form.querySelector('[type="submit"]'); button.disabled = true;
    try {
      if (form.id === "transfer-settings-form") await api("/api/transfer-settings", { method: "PUT", body: JSON.stringify({ autoReceive: $("#transfer-auto").checked, directory: $("#transfer-directory").value.trim() }) });
      else await api("/api/transfers/" + encodeURIComponent(form.dataset.task) + "?files=" + $("#transfer-delete-files").checked, { method: "DELETE" });
      closeModal(); await refreshTransfers(); toast(form.id === "transfer-settings-form" ? "随传设置已保存" : "记录已移除");
    } catch (error) { toast(error.message, true); button.disabled = false; }
  });
  $("#transfer-files").addEventListener("change", event => chooseTransferFiles(event.target.files));
  $("#transfer-peer").addEventListener("change", updateTransferSelection);
  const zone = $("#transfer-drop");
  zone.addEventListener("dragover", event => { event.preventDefault(); zone.classList.add("dragging"); });
  zone.addEventListener("dragleave", () => zone.classList.remove("dragging"));
  zone.addEventListener("drop", event => {
    event.preventDefault(); zone.classList.remove("dragging");
    if ([...event.dataTransfer.items].some(item => item.webkitGetAsEntry?.()?.isDirectory)) return toast("本版支持普通文件，请先将文件夹打包", true);
    chooseTransferFiles(event.dataTransfer.files);
  });
  window.addEventListener("hashchange", routeWorkspace);
  window.addEventListener("beforeunload", event => { if (transferUI.uploading) { event.preventDefault(); event.returnValue = ""; } });
  renderTransferDevices(); refreshTransfers(); routeWorkspace();
  setInterval(() => { renderTransferDevices(); refreshTransfers(); }, 2500);
}
