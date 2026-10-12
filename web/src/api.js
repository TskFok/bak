// 所有请求都带会话 cookie。后端出错时返回纯文本，字段校验失败时返回 {"fields": {...}}。
// 首次设置接口返回的是 {"Fields": {...}}，这里两种写法都接受。

export class ApiError extends Error {
  constructor(message, status, fields) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fields = fields || {};
  }
}

let unauthorizedHandler = null;

export function setUnauthorizedHandler(fn) {
  unauthorizedHandler = fn;
}

async function readBody(res) {
  if (res.status === 204) return null;
  const text = await res.text();
  if (!text) return null;
  const type = res.headers.get("Content-Type") || "";
  if (type.includes("application/json")) {
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  }
  return text.trim();
}

// options: method、body（对象会被序列化为 JSON）、public（为 true 时 401 不触发退出登录）
export async function request(path, options = {}) {
  const { body, public: isPublic, ...rest } = options;
  const init = {
    ...rest,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(rest.headers || {}) },
  };
  if (body !== undefined) {
    init.body = typeof body === "string" ? body : JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, init);
  } catch {
    throw new ApiError("无法连接到本地服务，请确认服务正在运行", 0);
  }
  const data = await readBody(res);
  if (res.ok) return data;

  if (res.status === 401 && !isPublic && unauthorizedHandler) {
    unauthorizedHandler();
  }
  const fields = data && typeof data === "object" ? data.fields || data.Fields : null;
  let message = typeof data === "string" && data ? data : "";
  if (!message) {
    message = fields ? "请检查表单中的错误" : `请求失败（${res.status}）`;
  }
  throw new ApiError(message, res.status, fields);
}
