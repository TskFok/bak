// 空路径让服务从用户主目录开始列目录。
export function dirsQuery(dir) {
  const path = (dir || "").trim();
  if (!path) return "/api/dirs";
  return `/api/dirs?path=${encodeURIComponent(path)}`;
}
