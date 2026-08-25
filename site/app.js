const RELEASE_API = "https://api.github.com/repos/Leoforever123/Polysync/releases/latest";

const platform = (() => {
  const value = `${navigator.userAgent} ${navigator.platform}`.toLowerCase();
  if (value.includes("win")) return "windows";
  if (value.includes("mac")) return "mac";
  if (value.includes("linux")) return "linux";
  return "unknown";
})();

function selectRecommendation() {
  const cards = [...document.querySelectorAll(".download-card")];
  const match = cards.find((card) => {
    const asset = card.dataset.asset || "";
    if (platform === "windows") return asset.includes("windows");
    if (platform === "mac") return asset.includes("darwin-arm64");
    if (platform === "linux") return asset.includes("linux-amd64");
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
    target.scrollIntoView({ behavior: "smooth", block: "start" });
  });
});

selectRecommendation();
refreshRelease().then(selectRecommendation);
