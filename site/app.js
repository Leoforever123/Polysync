const RELEASE_API = "https://api.github.com/repos/Leoforever123/Polysync/releases/latest";

const platform = (() => {
  const value = `${navigator.userAgent} ${navigator.platform}`.toLowerCase();
  if (value.includes("win")) return "windows";
  if (value.includes("mac")) return "mac";
  if (value.includes("linux")) return "linux";
  return "unknown";
})();

function selectRecommendation() {
  // Linux distributions need different packages; let the user choose on-page.
  if (platform === "linux") {
    document.querySelector("#recommended-download").href = "#downloads";
    document.querySelector("#recommended-label").textContent = "选择 Ubuntu、Arch 或通用版";
    return;
  }
  const cards = [...document.querySelectorAll(".download-card")];
  const match = cards.find((card) => {
    const asset = card.dataset.asset || "";
    if (platform === "windows") return asset.includes("windows");
    if (platform === "mac") return asset.includes("darwin-arm64");
    return false;
  });

  if (!match) return;
  match.classList.add("recommended");
  const button = document.querySelector("#recommended-download");
  const label = document.querySelector("#recommended-label");
  button.href = match.href;
  button.dataset.asset = match.dataset.asset;
  label.textContent = `适用于 ${match.querySelector("strong").textContent}`;
}

async function refreshRelease() {
  try {
    const response = await fetch(RELEASE_API, { headers: { Accept: "application/vnd.github+json" } });
    if (!response.ok) return;
    const release = await response.json();
    const assets = new Map(release.assets.map((asset) => [asset.name, asset.browser_download_url]));

    document.querySelectorAll("[data-version]").forEach((element) => {
      element.textContent = release.tag_name;
    });
    document.querySelectorAll("[data-asset]").forEach((element) => {
      const patterns = {
        "ubuntu-amd64": /^polysync_[0-9.]+-\d+_amd64\.deb$/,
        "ubuntu-arm64": /^polysync_[0-9.]+-\d+_arm64\.deb$/,
        "arch-x86_64": /^polysync-[0-9.]+-\d+-x86_64\.pkg\.tar\.zst$/,
      };
      const pattern = patterns[element.dataset.package];
      const packageName = pattern && [...assets.keys()].find(name => pattern.test(name));
      if (packageName) {
        element.dataset.asset = packageName;
        document.querySelectorAll("[data-install]").forEach(command => {
          if (command.dataset.install === element.dataset.package) {
            command.textContent = `${element.dataset.package === "arch-x86_64" ? "sudo pacman -U" : "sudo apt install"} ./${packageName}`;
          }
        });
      }
      if (element.dataset.portable !== "true" && element.dataset.asset === "polysync-windows-amd64.exe" && assets.has("polysync-windows-amd64.msi")) {
        element.dataset.asset = "polysync-windows-amd64.msi";
        const detail = element.querySelector("small");
        if (detail) detail.textContent = "x64 · MSI 安装包";
      }
      const url = assets.get(element.dataset.asset);
      if (url) element.href = url;
    });
  } catch {
    // Static links remain available when GitHub API is unreachable.
  }
}

document.querySelectorAll('a[href^="#"]').forEach((link) => {
  link.addEventListener("click", (event) => {
    const target = document.querySelector(link.getAttribute("href"));
    if (!target) return;
    event.preventDefault();
    target.scrollIntoView({ behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth", block: "start" });
  });
});

selectRecommendation();
refreshRelease().then(selectRecommendation);
