const RELEASE_API = "https://api.github.com/repos/Leoforever123/Polysync/releases/latest";
const packagePatterns = {
  "ubuntu-amd64": /^polysync_[\d.]+-\d+_amd64\.deb$/,
  "ubuntu-arm64": /^polysync_[\d.]+-\d+_arm64\.deb$/,
  "arch-x86_64": /^polysync-[\d.]+-\d+-x86_64\.pkg\.tar\.zst$/,
};

function detectPlatform(device) {
  const ua = device.userAgent || "";
  if (/iPhone|iPad|iPod/i.test(ua) || (/Mac/.test(device.platform) && device.maxTouchPoints > 1)) return "ios";
  if (/Android/i.test(ua)) return "android";
  const os = device.userAgentData?.platform || device.platform || ua;
  if (/Win/i.test(os)) return "windows";
  if (/Mac/i.test(os)) return "mac";
  if (/Linux/i.test(os)) return "linux";
  return "unknown";
}

const platform = detectPlatform(navigator);

function selectRecommendation() {
  const cards = [...document.querySelectorAll(".download-card")];
  for (const card of cards) card.classList.remove("recommended");
  const match = cards.find(card => card.dataset.platform === platform && !card.dataset.unavailable);
  const ownButton = document.querySelector("#recommended-download");
  if (!ownButton) return;
  const label = document.querySelector("#recommended-label");
  const automatic = match && platform === "windows";
  ownButton.href = automatic ? match.href : "#downloads";
  if (automatic) {
    match.classList.add("recommended");
    label.textContent = "Windows x64";
  } else {
    label.textContent = ({
      android: "查看 Android 实验版安装说明",
      mac: "选择 Apple 芯片或 Intel 版本",
      linux: "选择 Ubuntu、Arch 或通用版",
      ios: "iOS 暂无客户端，可在电脑上安装",
    })[platform] || "选择你的平台";
  }
}

const picker = document.querySelector("#download-platform");
function filterDownloads() {
  const selection = picker?.value || "all";
  for (const card of document.querySelectorAll(".download-card")) {
    card.hidden = Boolean(card.dataset.unavailable) || (selection !== "all" && selection !== card.dataset.platform);
  }
  for (const note of document.querySelectorAll("[data-note]")) {
    note.hidden = selection !== "all" && !note.dataset.note.split(" ").includes(selection);
  }
}

if (picker) {
  const known = ["windows", "mac", "linux", "android"].includes(platform);
  picker.value = known ? platform : "all";
  document.querySelector("#platform-picker").hidden = false;
  picker.addEventListener("change", filterDownloads);
}

async function refreshRelease() {
  try {
    const response = await fetch(RELEASE_API, { headers: { Accept: "application/vnd.github+json" } });
    if (!response.ok) return;
    const release = await response.json();
    if (!release.tag_name || !Array.isArray(release.assets) || !release.assets.length) return;
    const assets = new Map(release.assets.map(asset => [asset.name, asset.browser_download_url]));
    for (const element of document.querySelectorAll("[data-asset]")) {
      let name = element.dataset.asset;
      const pattern = packagePatterns[element.dataset.package];
      if (pattern) name = [...assets.keys()].find(candidate => pattern.test(candidate));
      if (element.dataset.platform === "windows" && element.dataset.portable !== "true") {
        name = assets.has("polysync-windows-amd64.msi") ? "polysync-windows-amd64.msi" : "polysync-windows-amd64.exe";
        element.querySelector("small").textContent = name.endsWith(".msi") ? "x64 · MSI 安装包" : "x64 · 免安装 EXE";
      }
      const url = assets.get(name);
      if (!url) {
        // The experimental Android channel has its own release and installation identity.
        if (element.dataset.platform !== "android") {
          element.dataset.unavailable = "true";
          element.hidden = true;
        }
        continue;
      }
      delete element.dataset.unavailable;
      element.dataset.asset = name;
      element.href = url;
      if (pattern) {
        for (const command of document.querySelectorAll("[data-install]")) {
          if (command.dataset.install === element.dataset.package) {
            const installer = element.dataset.package === "arch-x86_64" ? "sudo pacman -U" : "sudo apt install";
            command.textContent = installer + " ./" + name;
          }
        }
      }
    }
    for (const element of document.querySelectorAll("[data-version]")) element.textContent = release.tag_name;
  } catch {
    // Offline visitors can still use the explicitly versioned links shipped with the page.
  }
}
filterDownloads();
selectRecommendation();
refreshRelease().then(() => { filterDownloads(); selectRecommendation(); });
