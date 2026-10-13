// 空路径让服务从用户主目录开始列目录。
export function dirsQuery(dir) {
  const path = (dir || "").trim();
  if (!path) return "/api/dirs";
  return `/api/dirs?path=${encodeURIComponent(path)}`;
}

export const RECENT_DIRS_KEY = "bak.recentDirs";
const RECENT_DIRS_LIMIT = 8;

// normalizeRecentDirs 去掉空白、非字符串和重复项，保留先出现的路径。
function normalizeRecentDirs(recent, limit = RECENT_DIRS_LIMIT) {
  const seen = new Set();
  const out = [];
  const items = Array.isArray(recent) ? recent : [];
  for (const item of items) {
    if (typeof item !== "string") continue;
    const path = item.trim();
    if (!path || seen.has(path)) continue;
    seen.add(path);
    out.push(path);
    if (out.length >= limit) break;
  }
  return out;
}

// rememberDir 把选中的目录放到最近列表最前。
export function rememberDir(recent, path, limit = RECENT_DIRS_LIMIT) {
  const next = typeof path === "string" ? path.trim() : "";
  if (!next) return normalizeRecentDirs(recent, limit);
  return normalizeRecentDirs([next, ...(Array.isArray(recent) ? recent : [])], limit);
}

// recentDirUnreadable 表示服务明确拒绝了这个路径。网络错误不删最近记录。
export function recentDirUnreadable(status) {
  return status === 400;
}

// forgetDir 去掉打不开的最近路径。
export function forgetDir(recent, path) {
  const drop = typeof path === "string" ? path.trim() : "";
  return normalizeRecentDirs(recent).filter((item) => item !== drop);
}

export function readRecentDirs(storage) {
  try {
    if (!storage) return [];
    const raw = storage.getItem(RECENT_DIRS_KEY);
    if (!raw) return [];
    return normalizeRecentDirs(JSON.parse(raw));
  } catch {
    return [];
  }
}

export function writeRecentDirs(storage, recent) {
  if (!storage) return;
  try {
    storage.setItem(RECENT_DIRS_KEY, JSON.stringify(normalizeRecentDirs(recent)));
  } catch {
    // 隐私模式或配额满时忽略，选择目录本身仍可继续。
  }
}

// dirBaseName 取路径最后一段，供最近列表显示短名称。
export function dirBaseName(path) {
  const raw = String(path || "").trim();
  const trimmed = raw.replace(/[\\/]+$/, "");
  if (!trimmed) return raw;
  const parts = trimmed.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || raw;
}
