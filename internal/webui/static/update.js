const updateUI = { token: "", timer: null, generation: 0, installing: false };
function updateButtonState(s) {
  const busy = ["checking", "downloading", "installing"].includes(s.phase);
  return { check: !busy, download: !busy && s.available && s.supported && s.phase !== "ready", install: s.phase === "ready" && s.supported };
}
function showUpdateState(s) {
  if (state.modal !== "update") return;
  updateUI.token = s.token;
  $("#update-current").textContent = s.current;
  $("#update-latest").textContent = s.latest ? "最新正式版本：" + s.latest : "尚未检查最新版本";
  $("#update-message").textContent = s.message;
  $("#update-platform").hidden = s.supported;
  const buttons = updateButtonState(s);
  for (const action of ["check", "download", "install"]) $("#update-" + action).disabled = !buttons[action];
  $("#update-download").hidden = !buttons.download && s.phase !== "downloading";
  $("#update-install").hidden = !buttons.install && s.phase !== "installing";
  $("#update-progress").hidden = s.phase !== "downloading";
  $("#update-progress").max = s.total || 1;
  $("#update-progress").value = s.downloaded || 0;
  $("#update-progress").setAttribute("aria-valuetext", s.total ? Math.floor(100 * s.downloaded / s.total) + "%" : "正在准备下载");
  updateUI.installing = s.phase === "installing";
}
async function refreshUpdate(generation) {
  if (state.modal !== "update" || generation !== updateUI.generation) return;
  try {
    const s = await api("/api/update");
    if (generation !== updateUI.generation) return;
    showUpdateState(s);
  } catch (error) {
    if (state.modal === "update" && generation === updateUI.generation) $("#update-message").textContent = updateUI.installing ? "正在安装并重启，等待服务重新连接…" : "无法读取更新状态：" + error.message;
  }
  if (state.modal === "update" && generation === updateUI.generation) updateUI.timer = setTimeout(() => refreshUpdate(generation), 1500);
}
function openUpdates() {
  clearTimeout(updateUI.timer);
  const generation = ++updateUI.generation;
  updateUI.token = "";
  openModal(`<h2 id="modal-title">版本与更新</h2><p class="modal-intro">当前版本 <strong id="update-current">正在读取…</strong></p><p id="update-latest"></p><p id="update-message" role="status">正在读取更新状态…</p><progress id="update-progress" aria-label="安装包下载进度" hidden></progress><p class="muted" id="update-platform" hidden>当前系统暂不支持应用内安装，请从官网下载对应版本。</p><p class="muted">下载完成后可安装并重启。正在同步或传输时，请完成任务后再安装。</p><div class="modal-actions"><button class="button ghost" id="update-check" disabled>检查更新</button><button class="button ghost" id="update-download" hidden disabled>下载安装包</button><button class="button primary" id="update-install" hidden disabled>安装并重启</button></div>`, "update");
  refreshUpdate(generation);
}
if (typeof document !== "undefined") {
  document.addEventListener("click", async event => {
    const id = event.target.closest("button")?.id;
    if (id === "open-update") { openUpdates(); return; }
    if (!["update-check", "update-download", "update-install"].includes(id) || !updateUI.token) return;
    const generation = updateUI.generation;
    clearTimeout(updateUI.timer);
    for (const action of ["check", "download", "install"]) $("#update-" + action).disabled = true;
    $("#update-message").textContent = id === "update-install" ? "正在准备安装…" : "正在处理…";
    try {
      const s = await api("/api/update/" + id.slice(7), { method: "POST", headers: { "Content-Type": "application/json", "X-PolySync-Update-Token": updateUI.token }, body: "{}" });
      if (generation === updateUI.generation) showUpdateState(s);
    } catch (error) {
      if (state.modal === "update" && generation === updateUI.generation) $("#update-message").textContent = error.message;
    }
    if (state.modal === "update" && generation === updateUI.generation) updateUI.timer = setTimeout(() => refreshUpdate(generation), 1500);
  });
  window.addEventListener("hashchange", () => { if (location.hash === "#updates") openUpdates(); });
  if (location.hash === "#updates") openUpdates();
  new MutationObserver(() => { if (state.modal !== "update") { clearTimeout(updateUI.timer); updateUI.generation++; } }).observe($("#modal-backdrop"), { attributes: true, attributeFilter: ["class"] });
}
if (typeof module !== "undefined") module.exports = { updateButtonState };
