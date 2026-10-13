// 从运行记录的进度说明里取出压缩进度。格式由打包流程写成「正在压缩 3/120」。
export function parsePackProgress(note) {
  const match = /^正在压缩 (\d+)\/(\d+)$/.exec(String(note ?? ""));
  if (!match) return null;
  const done = Number(match[1]);
  const total = Number(match[2]);
  if (!Number.isSafeInteger(done) || !Number.isSafeInteger(total) || total < 1 || done > total) {
    return null;
  }
  return { done, total, percent: Math.round((done / total) * 100) };
}

export const PACK_UPLOAD_NOTE = "正在上传压缩包";
