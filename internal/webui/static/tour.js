const tourSteps = [
  { target: "#device-card", title: "这是你的当前设备", text: "设备名称帮助你认出这台电脑。连接地址可用于手动配对，点击「修改名称」可以换个好记的名字。" },
  { target: "#add-device", title: "先连接另一台设备", text: "点击「添加设备」发现同一局域网中的电脑，也可以输入对方的地址。两台电脑都需要运行 PolySync。" },
  { target: "#device-list", title: "配对一次，之后放心传", text: "首次连接时，在对方允许请求后输入六位验证码。成功后设备会显示「已安全配对」。这里先了解流程，不需要现在完成配对。" },
  { target: "#add-share", title: "添加要同步的文件夹", text: "配对完成后，点击「添加同步」，选择目标设备和这台电脑上的文件夹，填写显示名称，再点击「发送邀请」。可开启「在线时自动同步」并设置检查间隔。" },
  { target: "#share-list", title: "在另一台电脑接受邀请", text: "发送后，这里会显示「等待对方接受邀请」。请在另一台电脑的 PolySync 页面点击邀请中的「选择文件夹」，选好对应的本地文件夹，再点击「接受并同步」，开始首次同步。" },
  { target: "#share-list", title: "在这里查看和管理同步", text: "文件夹卡片显示同步进度、上次同步时间或失败原因。可以点击「立即同步」，也可以用「自动」开关控制定时检查。尚未添加文件夹时，这里会显示添加入口。" },
  { target: "#workspace-nav", title: "同步，或只是随手传", text: "「同步」让文件夹持续保持一致；「随传」用于临时发送文件，已配对设备自动接收，在工作台另存即可。" },
  { target: "#background-note", title: "关掉页面，后台仍在", text: "Windows 右下角的 PolySync 图标是后台入口：双击打开页面，右键可立即同步、发送文件或退出应用。上传交给后台后，关掉页面也能继续发送。" }
];
const tourState = { index: -1, returnFocus: null, key: "polysync-tour-v1" };
function tourStorage(value) { try { if (value) localStorage.setItem(tourState.key, value); return localStorage.getItem(tourState.key); } catch { return null; } }
function positionTour() {
  if (tourState.index < 0) return;
  const rect = $(tourSteps[tourState.index].target).getBoundingClientRect();
  const spotlight = $("#tour-spotlight"), bubble = $("#tour-bubble");
  Object.assign(spotlight.style, { left: rect.left - 6 + "px", top: rect.top - 6 + "px", width: rect.width + 12 + "px", height: rect.height + 12 + "px" });
  const width = bubble.offsetWidth, height = bubble.offsetHeight, gap = 18;
  const top = rect.bottom + gap + height <= innerHeight - 12 ? rect.bottom + gap : rect.top - gap - height;
  Object.assign(bubble.style, { left: Math.max(12, Math.min(innerWidth - width - 12, rect.left)) + "px", top: Math.max(12, Math.min(innerHeight - height - 12, top)) + "px" });
}
function showTourStep(index) {
  tourState.index = index;
  const step = tourSteps[index];
  $("#tour-title").textContent = step.title;
  $("#tour-copy").textContent = step.target === "#background-note" && state.data && !state.data.trayEnabled ? "关闭页面后服务仍在运行。当前实例未开启 Windows 托盘，请回到启动程序的终端按 Ctrl+C 退出。上传交给后台后，可以放心关闭页面。" : step.text;
  $("#tour-count").textContent = (index + 1) + " / " + tourSteps.length;
  $("#tour-prev").disabled = index === 0;
  $("#tour-next").textContent = index === tourSteps.length - 1 ? "开始使用" : "下一步";
  tourState.observer?.disconnect();
  if (typeof ResizeObserver !== "undefined") { tourState.observer = new ResizeObserver(positionTour); tourState.observer.observe($(step.target)); }
  $(step.target).scrollIntoView({ block: "center", behavior: "instant" });
  positionTour(); requestAnimationFrame(positionTour);
  $("#tour-next").focus({ preventScroll: true });
}
function startTour() {
  if (state.modal) { tourState.pending = true; return; }
  tourState.pending = false;
  if (tourState.index >= 0) return;
  tourState.returnFocus = document.activeElement;
  setWorkspace("sync"); $("#tour-invite").hidden = true;
  $("#tour-layer").hidden = false; $(".shell").inert = true;
  showTourStep(0);
}
function endTour(status = "skipped") {
  tourStorage(status);
  tourState.observer?.disconnect();
  tourState.index = -1; $("#tour-layer").hidden = true; $(".shell").inert = false;
  $("#tour-invite").hidden = true;
  const target = tourState.returnFocus;
  if (target?.isConnected && target.getClientRects().length) target.focus(); else $("#help-tour").focus();
  if (location.hash === "#tour") history.replaceState(null, "", "#sync");
}
if (typeof document !== "undefined") {
  $("#tour-invite").hidden = !!tourStorage();
  document.addEventListener("click", event => {
    const id = event.target.closest("button")?.id;
    if (id === "help-tour" || id === "tour-start") startTour();
    if (id === "tour-dismiss") { tourStorage("skipped"); $("#tour-invite").hidden = true; }
    if (id === "tour-skip") endTour();
    if (id === "tour-prev" && tourState.index > 0) showTourStep(tourState.index - 1);
    if (id === "tour-next") { if (tourState.index === tourSteps.length - 1) endTour("completed"); else showTourStep(tourState.index + 1); }
  });
  document.addEventListener("keydown", event => {
    if (event.defaultPrevented) return;
    if (tourState.index < 0) return;
    if (event.key === "Escape") { event.preventDefault(); endTour(); }
    if (event.key === "Tab") {
      const nodes = [...$("#tour-bubble").querySelectorAll("button:not(:disabled)")];
      const first = nodes[0], last = nodes.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
  });
  new MutationObserver(() => { if (tourState.pending && !state.modal) startTour(); }).observe($("#modal-backdrop"), { attributes: true, attributeFilter: ["class"] });
  window.addEventListener("resize", positionTour);
  window.addEventListener("scroll", positionTour, { passive: true });
  if (location.hash === "#tour") startTour();
}
