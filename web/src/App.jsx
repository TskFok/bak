import { useCallback, useEffect, useId, useRef, useState } from "react";
import { ApiError, request, setUnauthorizedHandler } from "./api.js";

const POLL_MS = 3000;

const PROVIDER_NAMES = {
  baidu: "百度网盘",
  aliyun: "阿里云盘",
  webdav: "WebDAV",
};

// 运行状态一律用文字表达，颜色和图标只是补充。
const RUN_STATUS = {
  success: { text: "成功", icon: "check", tone: "success" },
  partial: { text: "部分成功", icon: "alert", tone: "partial" },
  failed: { text: "失败", icon: "x", tone: "failed" },
  running: { text: "进行中", icon: "clock", tone: "running" },
  skipped: { text: "跳过", icon: "skip", tone: "skipped" },
};

const CLOUD_STATUS = {
  connected: { text: "已连接", icon: "check", tone: "success" },
  configured: { text: "已保存，未测试", icon: "clock", tone: "running" },
  disconnected: { text: "未配置", icon: "skip", tone: "skipped" },
  error: { text: "连接失败", icon: "x", tone: "failed" },
};

const TRIGGER_TEXT = { manual: "手动", schedule: "定时" };

const NAV = [
  { page: "overview", href: "#/", label: "概览", icon: "home" },
  { page: "clouds", href: "#/clouds", label: "网盘", icon: "cloud" },
  { page: "tasks", href: "#/tasks", label: "备份任务", icon: "tasks" },
  { page: "runs", href: "#/runs", label: "运行记录", icon: "history" },
];

/* ---------- 图标（内联 SVG） ---------- */

const ICON_PATHS = {
  home: <path d="M3 11l9-8 9 8M5 10v10h5v-6h4v6h5V10" />,
  cloud: <path d="M7 18a4 4 0 0 1-.6-7.96A5.5 5.5 0 0 1 17 8.5a4.75 4.75 0 0 1 0 9.5H7z" />,
  tasks: (
    <>
      <path d="M9 6h11M9 12h11M9 18h11" />
      <path d="M3.5 6l1 1 2-2M3.5 12l1 1 2-2M3.5 18l1 1 2-2" />
    </>
  ),
  history: (
    <>
      <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
      <path d="M3 3v5h5M12 7v5l3 2" />
    </>
  ),
  logout: <path d="M9 4H5v16h4M16 8l4 4-4 4M20 12H9" />,
  check: <path d="M5 12.5l4.5 4.5L19 7.5" />,
  alert: <path d="M12 4l9 16H3L12 4zM12 10v4M12 17.5v.5" />,
  x: <path d="M6 6l12 12M18 6L6 18" />,
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  skip: <path d="M5 5l9 7-9 7V5zM18 5v14" />,
  play: <path d="M7 4l13 8-13 8V4z" />,
  pause: <path d="M8 5v14M16 5v14" />,
  trash: <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />,
};

function Icon({ name, size = 20 }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {ICON_PATHS[name]}
    </svg>
  );
}

/* ---------- 通用小工具 ---------- */

function formatTime(value) {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "—";
  return d.toLocaleString("zh-CN", { hour12: false });
}

function errorText(err) {
  return err instanceof ApiError ? err.message : "操作失败，请重试";
}

function usePoll(fn, enabled) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    if (!enabled) return undefined;
    const id = setInterval(() => ref.current(), POLL_MS);
    return () => clearInterval(id);
  }, [enabled]);
}

// 加载数据。key 变化时重新加载并显示加载中；reload() 静默刷新，不闪烁。
function useLoad(loader, key = "") {
  const [state, setState] = useState({ data: null, error: "", loading: true });
  const loaderRef = useRef(loader);
  loaderRef.current = loader;
  const seq = useRef(0);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const data = await loaderRef.current();
      if (alive.current && mine === seq.current) setState({ data, error: "", loading: false });
    } catch (err) {
      if (alive.current && mine === seq.current) {
        setState((s) => ({ ...s, error: errorText(err), loading: false }));
      }
    }
  }, []);

  useEffect(() => {
    setState({ data: null, error: "", loading: true });
    reload();
  }, [key, reload]);

  return { ...state, reload };
}

function StatusBadge({ status, table = RUN_STATUS }) {
  const info = table[status] || { text: status || "未知", icon: "alert", tone: "skipped" };
  return (
    <span className={`badge badge-${info.tone}`}>
      <Icon name={info.icon} size={16} />
      <span>{info.text}</span>
    </span>
  );
}

function TextField({ label, value, onChange, type = "text", error, hint, placeholder, autoComplete, required }) {
  const id = useId();
  const describedBy = [hint ? `${id}-hint` : "", error ? `${id}-err` : ""].filter(Boolean).join(" ");
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        type={type}
        value={value}
        placeholder={placeholder}
        autoComplete={autoComplete}
        required={required}
        aria-invalid={error ? "true" : undefined}
        aria-describedby={describedBy || undefined}
        onChange={(e) => onChange(e.target.value)}
      />
      {hint && (
        <p className="field-hint" id={`${id}-hint`}>
          {hint}
        </p>
      )}
      {error && (
        <p className="field-error" id={`${id}-err`} role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

function Message({ message }) {
  if (!message) return <div aria-live="polite" className="message-slot" />;
  return (
    <div aria-live="polite" className="message-slot">
      <p className={`message message-${message.kind}`}>
        <Icon name={message.kind === "ok" ? "check" : "alert"} size={18} />
        <span>{message.text}</span>
      </p>
    </div>
  );
}

/* ---------- 首次设置与登录 ---------- */

function AuthShell({ title, children }) {
  return (
    <main className="auth-page">
      <section className="auth-card">
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

function SetupForm({ onDone }) {
  const [form, setForm] = useState({ username: "", password: "", confirm: "" });
  const [fields, setFields] = useState({});
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setFields({});
    try {
      const res = await request("/api/setup", { method: "POST", body: form, public: true });
      onDone(res?.username || form.username);
    } catch (err) {
      setFields(err.fields || {});
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell title="设置本地账号">
      <p className="auth-intro">首次使用，请先创建登录本面板的账号。账号只保存在这台电脑上。</p>
      <form onSubmit={submit} noValidate>
        <TextField label="用户名" value={form.username} onChange={set("username")} error={fields.username} autoComplete="username" />
        <TextField label="密码" type="password" value={form.password} onChange={set("password")} error={fields.password} autoComplete="new-password" />
        <TextField label="确认密码" type="password" value={form.confirm} onChange={set("confirm")} error={fields.confirm} autoComplete="new-password" />
        {error && !Object.keys(fields).length && (
          <p className="message message-error" role="alert">
            <Icon name="alert" size={18} />
            <span>{error}</span>
          </p>
        )}
        <button className="btn btn-primary btn-block" type="submit" disabled={busy}>
          {busy ? "正在创建…" : "创建账号并进入"}
        </button>
      </form>
    </AuthShell>
  );
}

function LoginForm({ onDone }) {
  const [form, setForm] = useState({ username: "", password: "" });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await request("/api/login", { method: "POST", body: form, public: true });
      onDone(res?.username || form.username);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthShell title="登录 bak">
      <form onSubmit={submit} noValidate>
        <TextField label="用户名" value={form.username} onChange={(v) => setForm((f) => ({ ...f, username: v }))} autoComplete="username" />
        <TextField label="密码" type="password" value={form.password} onChange={(v) => setForm((f) => ({ ...f, password: v }))} autoComplete="current-password" />
        {error && (
          <p className="message message-error" role="alert">
            <Icon name="alert" size={18} />
            <span>{error}</span>
          </p>
        )}
        <button className="btn btn-primary btn-block" type="submit" disabled={busy}>
          {busy ? "正在登录…" : "登录"}
        </button>
      </form>
    </AuthShell>
  );
}

/* ---------- 概览 ---------- */

function RunTable({ runs, taskNames }) {
  if (!runs.length) return <p className="empty">还没有运行记录。</p>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th scope="col">任务</th>
            <th scope="col">状态</th>
            <th scope="col">触发方式</th>
            <th scope="col">开始时间</th>
            <th scope="col">成功 / 跳过 / 失败</th>
            <th scope="col">详情</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id}>
              <td>{taskNames?.[r.taskId] || `任务 ${r.taskId}`}</td>
              <td>
                <StatusBadge status={r.status} />
                {r.status === "running" && r.progressNote ? <div>{r.progressNote}</div> : null}
              </td>
              <td>{TRIGGER_TEXT[r.trigger] || r.trigger}</td>
              <td>{formatTime(r.startedAt)}</td>
              <td>
                {r.successCount} / {r.skipCount} / {r.failCount}
              </td>
              <td>
                <a href={`#/runs/${r.id}`}>查看详情<span className="sr-only">（运行 {r.id}）</span></a>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function OverviewPage() {
  const { data, error, loading, reload } = useLoad(() => request("/api/overview"));
  const running = !!data?.recentRuns?.some((r) => r.status === "running");
  usePoll(reload, running);

  return (
    <>
      <h1>概览</h1>
      {loading && <p role="status">正在加载…</p>}
      {error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{error}</span>
        </p>
      )}
      {data && (
        <>
          <section aria-labelledby="ov-clouds">
            <h2 id="ov-clouds">网盘状态</h2>
            <ul className="card-grid">
              {data.clouds.map((c) => (
                <li className="card" key={c.provider}>
                  <h3>
                    <Icon name="cloud" />
                    {PROVIDER_NAMES[c.provider] || c.provider}
                  </h3>
                  <StatusBadge status={c.status} table={CLOUD_STATUS} />
                  {c.lastError && <p className="field-error">{c.lastError}</p>}
                </li>
              ))}
            </ul>
          </section>
          <section aria-labelledby="ov-tasks">
            <h2 id="ov-tasks">备份任务</h2>
            <p className="big-number">
              <span>{data.taskCount}</span> 个任务
            </p>
            <a href="#/tasks">管理备份任务</a>
          </section>
          <section aria-labelledby="ov-runs">
            <h2 id="ov-runs">最近运行</h2>
            {running && <p role="status">有任务正在备份，每 3 秒自动刷新。</p>}
            <RunTable runs={data.recentRuns} />
          </section>
        </>
      )}
    </>
  );
}

/* ---------- 网盘 ---------- */

function CloudCard({ cloud, onChanged }) {
  const name = cloud.provider;
  const isWebdav = name === "webdav";
  const cfg = cloud.config || {};
  const [form, setForm] = useState({
    url: cfg.url || "",
    username: cfg.username || "",
    password: "",
    clientId: cfg.clientId || "",
    clientSecret: "",
    deviceId: cfg.deviceId || "",
  });
  const [fields, setFields] = useState({});
  const [msg, setMsg] = useState(null);
  const [authUrl, setAuthUrl] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState("");
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));
  const saved = cloud.id !== 0;

  async function run(kind, fn) {
    setBusy(kind);
    setMsg(null);
    try {
      await fn();
    } catch (err) {
      setMsg({ kind: "error", text: errorText(err) });
      setFields(err.fields || {});
    } finally {
      setBusy("");
    }
  }

  function save(e) {
    e.preventDefault();
    return run("save", async () => {
      setFields({});
      const body = isWebdav
        ? { url: form.url, username: form.username, password: form.password }
        : { clientId: form.clientId, clientSecret: form.clientSecret, deviceId: form.deviceId };
      await request(`/api/clouds/${name}`, { method: "PUT", body });
      setForm((f) => ({ ...f, password: "", clientSecret: "" }));
      setMsg({ kind: "ok", text: "配置已保存" });
      await onChanged();
    });
  }

  const test = () =>
    run("test", async () => {
      const res = await request(`/api/clouds/${name}/test`, { method: "POST" });
      if (res.status === "connected") setMsg({ kind: "ok", text: "连接成功" });
      else setMsg({ kind: "error", text: `连接失败：${res.lastError || "未知原因"}` });
      await onChanged();
    });

  const getAuthUrl = () =>
    run("url", async () => {
      const res = await request(`/api/clouds/${name}/authorize`, { method: "POST", body: { code: "" } });
      setAuthUrl(res.authorizeUrl);
    });

  const submitCode = (e) => {
    e.preventDefault();
    return run("code", async () => {
      await request(`/api/clouds/${name}/authorize`, { method: "POST", body: { code } });
      setCode("");
      setMsg({ kind: "ok", text: "授权成功" });
      await onChanged();
    });
  };

  return (
    <li className="card cloud-card">
      <header className="card-head">
        <h2>
          <Icon name="cloud" />
          {cloud.displayName || PROVIDER_NAMES[name]}
        </h2>
        <StatusBadge status={cloud.status} table={CLOUD_STATUS} />
      </header>
      {cloud.status === "error" && cloud.lastError && <p className="field-error">{cloud.lastError}</p>}

      <form onSubmit={save} noValidate>
        {isWebdav ? (
          <>
            <TextField label="WebDAV 地址" value={form.url} onChange={set("url")} error={fields.url} placeholder="https://example.com/dav" autoComplete="off" />
            <TextField label="用户名" value={form.username} onChange={set("username")} error={fields.username} autoComplete="off" />
            <TextField
              label="密码"
              type="password"
              value={form.password}
              onChange={set("password")}
              error={fields.password}
              hint={saved ? "留空表示继续使用已保存的密码" : undefined}
              autoComplete="new-password"
            />
          </>
        ) : (
          <>
            <TextField label={name === "baidu" ? "应用 Key（Client ID）" : "应用 ID（Client ID）"} value={form.clientId} onChange={set("clientId")} error={fields.clientId} autoComplete="off" />
            <TextField
              label="应用密钥（Client Secret）"
              type="password"
              value={form.clientSecret}
              onChange={set("clientSecret")}
              error={fields.clientSecret}
              hint={saved ? "留空表示继续使用已保存的密钥" : undefined}
              autoComplete="new-password"
            />
            {name === "baidu" && <TextField label="设备 ID（Device ID）" value={form.deviceId} onChange={set("deviceId")} error={fields.deviceId} autoComplete="off" />}
          </>
        )}
        <div className="btn-row">
          <button className="btn btn-primary" type="submit" disabled={!!busy}>
            {busy === "save" ? "正在保存…" : "保存配置"}
          </button>
          <button className="btn" type="button" onClick={test} disabled={!!busy || !saved}>
            {busy === "test" ? "正在测试…" : "测试连接"}
          </button>
        </div>
      </form>

      {!isWebdav && (
        <section className="auth-block" aria-label={`${PROVIDER_NAMES[name]}授权`}>
          <h3>授权</h3>
          {!saved && <p className="field-hint">请先保存配置，再进行授权。</p>}
          <button className="btn" type="button" onClick={getAuthUrl} disabled={!!busy || !saved}>
            {busy === "url" ? "正在获取…" : "获取授权链接"}
          </button>
          {authUrl && (
            <p className="auth-url">
              <a href={authUrl} target="_blank" rel="noreferrer">
                打开授权页面
              </a>
              <span className="url-text">{authUrl}</span>
            </p>
          )}
          <form onSubmit={submitCode} noValidate>
            <TextField label="授权码" value={code} onChange={setCode} autoComplete="off" />
            <button className="btn" type="submit" disabled={!!busy || !saved || !code.trim()}>
              {busy === "code" ? "正在提交…" : "提交授权码"}
            </button>
          </form>
        </section>
      )}

      <Message message={msg} />
    </li>
  );
}

function CloudsPage() {
  const { data, error, loading, reload } = useLoad(() => request("/api/clouds"));
  return (
    <>
      <h1>网盘</h1>
      {loading && <p role="status">正在加载…</p>}
      {error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{error}</span>
        </p>
      )}
      {data && (
        <ul className="card-grid card-grid-wide">
          {data.map((c) => (
            <CloudCard key={c.provider} cloud={c} onChanged={reload} />
          ))}
        </ul>
      )}
    </>
  );
}

/* ---------- 备份任务 ---------- */

const EMPTY_TASK = {
  name: "",
  localDir: "",
  provider: "",
  remoteDir: "/备份",
  scheduleTime: "02:00",
  enabled: true,
  packBeforeUpload: false,
  archivePassword: "",
  archiveByDate: false,
  archiveEncryption: "aes",
};

function taskToForm(task) {
  if (!task) return EMPTY_TASK;
  return {
    name: task.name || "",
    localDir: task.localDir || "",
    provider: task.provider || "",
    remoteDir: task.remoteDir || "/备份",
    scheduleTime: task.scheduleTime || "02:00",
    enabled: task.enabled !== false,
    packBeforeUpload: !!task.packBeforeUpload,
    archivePassword: "",
    archiveByDate: !!task.archiveByDate,
    archiveEncryption: task.archiveEncryption === "zipcrypto" ? "zipcrypto" : "aes",
  };
}

function packSummary(task) {
  if (!task.packBeforeUpload) return "按文件上传";
  const enc = task.hasArchivePassword ? (task.archiveEncryption === "zipcrypto" ? "ZipCrypto" : "AES-256") : "未加密";
  return task.archiveByDate ? `打包后备份，${enc}，按日期保留` : `打包后备份，${enc}`;
}

function TaskForm({ clouds, task, onSaved, onCancel }) {
  const editing = !!task;
  const [form, setForm] = useState(() => taskToForm(task));
  const [clearPassword, setClearPassword] = useState(false);
  const [fields, setFields] = useState({});
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));
  const selectId = useId();
  const enabledId = useId();
  const packId = useId();
  const dateId = useId();
  const encId = useId();
  const clearId = useId();
  const titleId = editing ? `edit-task-${task.id}` : "task-form-title";

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setFields({});
    const body = {
      name: form.name,
      localDir: form.localDir,
      provider: form.provider,
      remoteDir: form.remoteDir,
      scheduleTime: form.scheduleTime,
      enabled: form.enabled,
      packBeforeUpload: form.packBeforeUpload,
      archiveByDate: form.archiveByDate,
      archiveEncryption: form.archiveEncryption,
    };
    if (!editing) {
      body.archivePassword = form.packBeforeUpload ? form.archivePassword : "";
    } else if (form.archivePassword) {
      body.archivePassword = form.archivePassword;
    } else if (clearPassword) {
      body.archivePassword = "";
    }
    try {
      if (editing) {
        await request(`/api/tasks/${task.id}`, { method: "PUT", body });
      } else {
        await request("/api/tasks", { method: "POST", body });
        setForm(EMPTY_TASK);
        setClearPassword(false);
      }
      await onSaved();
    } catch (err) {
      setFields(err.fields || {});
      setError(Object.keys(err.fields || {}).length ? "" : errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={editing ? undefined : "card"} onSubmit={submit} noValidate aria-labelledby={titleId}>
      {editing ? <h3 id={titleId}>编辑任务</h3> : <h2 id={titleId}>新建备份任务</h2>}
      <TextField label="任务名称" value={form.name} onChange={set("name")} error={fields.name} hint="留空则使用本地目录名" autoComplete="off" />
      <TextField label="本地目录" value={form.localDir} onChange={set("localDir")} error={fields.localDir} placeholder="/Users/me/Documents" autoComplete="off" />
      <div className="field">
        <label htmlFor={selectId}>网盘</label>
        <select id={selectId} value={form.provider} onChange={(e) => set("provider")(e.target.value)} aria-invalid={fields.provider ? "true" : undefined}>
          <option value="">请选择网盘</option>
          {clouds.map((c) => (
            <option key={c.provider} value={c.provider} disabled={c.id === 0}>
              {c.displayName || PROVIDER_NAMES[c.provider]}
              {c.id === 0 ? "（未配置）" : ""}
            </option>
          ))}
        </select>
        {fields.provider && (
          <p className="field-error" role="alert">
            {fields.provider}
          </p>
        )}
      </div>
      <TextField label="网盘目录" value={form.remoteDir} onChange={set("remoteDir")} error={fields.remoteDir} hint="必须以 / 开头" autoComplete="off" />
      <TextField label="每日备份时刻" type="time" value={form.scheduleTime} onChange={set("scheduleTime")} error={fields.scheduleTime} />
      <div className="field field-check">
        <input id={packId} type="checkbox" checked={form.packBeforeUpload} onChange={(e) => set("packBeforeUpload")(e.target.checked)} />
        <label htmlFor={packId}>打包后备份</label>
      </div>
      <p className="field-hint">开启后先把整个目录打成一个 zip，再上传这一个压缩包。</p>
      {form.packBeforeUpload && (
        <>
          <TextField
            label="压缩包密码"
            type="password"
            value={form.archivePassword}
            onChange={(v) => {
              set("archivePassword")(v);
              if (v) setClearPassword(false);
            }}
            error={fields.archivePassword}
            hint={
              editing && task.hasArchivePassword && !clearPassword
                ? "留空则保持原密码。填写新密码会替换原来的。不设密码时，访达和资源管理器都可以直接打开。"
                : "留空则不加密，访达和资源管理器都可以直接打开。"
            }
            autoComplete="new-password"
          />
          {editing && task.hasArchivePassword && (
            <div className="field field-check">
              <input
                id={clearId}
                type="checkbox"
                checked={clearPassword}
                onChange={(e) => {
                  setClearPassword(e.target.checked);
                  if (e.target.checked) set("archivePassword")("");
                }}
              />
              <label htmlFor={clearId}>清除已保存的密码</label>
            </div>
          )}
          <div className="field">
            <label htmlFor={encId}>压缩加密</label>
            <select id={encId} value={form.archiveEncryption} onChange={(e) => set("archiveEncryption")(e.target.value)} aria-invalid={fields.archiveEncryption ? "true" : undefined} aria-describedby={`${encId}-hint`}>
              <option value="aes">AES-256（更安全，需 7-Zip 或 Keka）</option>
              <option value="zipcrypto">ZipCrypto（Windows 资源管理器可打开）</option>
            </select>
            <p className="field-hint" id={`${encId}-hint`}>
              只有填写了密码才会加密。访达不能打开任何带密码的 zip。ZipCrypto 可供 Windows 资源管理器直接打开。
            </p>
            {fields.archiveEncryption && (
              <p className="field-error" role="alert">
                {fields.archiveEncryption}
              </p>
            )}
          </div>
          <div className="field field-check">
            <input id={dateId} type="checkbox" checked={form.archiveByDate} onChange={(e) => set("archiveByDate")(e.target.checked)} />
            <label htmlFor={dateId}>按日期命名，保留每次备份的压缩包</label>
          </div>
        </>
      )}
      <div className="field field-check">
        <input id={enabledId} type="checkbox" checked={form.enabled} onChange={(e) => set("enabled")(e.target.checked)} />
        <label htmlFor={enabledId}>启用定时备份</label>
      </div>
      {error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{error}</span>
        </p>
      )}
      <div className="btn-row">
        <button className="btn btn-primary" type="submit" disabled={busy}>
          {busy ? "正在保存…" : editing ? "保存修改" : "保存任务"}
        </button>
        {editing && (
          <button className="btn" type="button" onClick={onCancel} disabled={busy}>
            取消
          </button>
        )}
      </div>
    </form>
  );
}

function TaskRow({ task, clouds, onChanged }) {
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState(null);
  const [editing, setEditing] = useState(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const cloudName = clouds.find((c) => c.provider === task.provider)?.displayName || PROVIDER_NAMES[task.provider] || task.provider;

  async function act(kind, fn) {
    setBusy(kind);
    setMsg(null);
    try {
      await fn();
    } catch (err) {
      if (alive.current) {
        setMsg({ kind: "error", text: err.status === 409 ? "该任务正在备份" : errorText(err) });
      }
    } finally {
      if (alive.current) setBusy("");
    }
  }

  const runNow = () =>
    act("run", async () => {
      const run = await request(`/api/tasks/${task.id}/run`, { method: "POST" });
      if (run.status === "running") {
        // 后端已接手，备份在后台继续；跳到运行详情，由那里轮询进度。
        if (alive.current) setMsg({ kind: "ok", text: "已开始备份" });
        window.location.hash = `#/runs/${run.id}`;
        return;
      }
      const text = `备份${RUN_STATUS[run.status]?.text || run.status}：成功 ${run.successCount}，跳过 ${run.skipCount}，失败 ${run.failCount}`;
      if (alive.current) setMsg({ kind: run.status === "failed" ? "error" : "ok", text: run.errorSummary ? `${text}（${run.errorSummary}）` : text });
      await onChanged();
    });

  const toggle = () =>
    act("toggle", async () => {
      const body = {
        name: task.name,
        localDir: task.localDir,
        provider: task.provider,
        remoteDir: task.remoteDir,
        scheduleTime: task.scheduleTime,
        enabled: !task.enabled,
      };
      await request(`/api/tasks/${task.id}`, { method: "PUT", body });
      await onChanged();
    });

  const remove = () => {
    if (!window.confirm(`确定删除任务“${task.name}”吗？`)) return Promise.resolve();
    return act("delete", async () => {
      await request(`/api/tasks/${task.id}`, { method: "DELETE" });
      await onChanged();
    });
  };

  return (
    <li className="card task-row">
      <header className="card-head">
        <h3>{task.name}</h3>
        <span className={`badge ${task.enabled ? "badge-success" : "badge-skipped"}`}>
          <Icon name={task.enabled ? "check" : "pause"} size={16} />
          <span>{task.enabled ? "定时已启用" : "定时已暂停"}</span>
        </span>
      </header>
      <dl className="meta">
        <div>
          <dt>本地目录</dt>
          <dd>{task.localDir}</dd>
        </div>
        <div>
          <dt>网盘</dt>
          <dd>{cloudName}</dd>
        </div>
        <div>
          <dt>网盘目录</dt>
          <dd>{task.remoteDir}</dd>
        </div>
        <div>
          <dt>每日时刻</dt>
          <dd>{task.scheduleTime}</dd>
        </div>
        <div>
          <dt>上传方式</dt>
          <dd>{packSummary(task)}</dd>
        </div>
      </dl>
      {editing && (
        <TaskForm
          clouds={clouds}
          task={task}
          onSaved={async () => {
            setEditing(false);
            await onChanged();
          }}
          onCancel={() => setEditing(false)}
        />
      )}
      <div className="btn-row">
        <button className="btn btn-primary" type="button" onClick={runNow} disabled={!!busy}>
          <Icon name="play" size={18} />
          {busy === "run" ? "正在备份…" : "立即备份"}
        </button>
        <button className="btn" type="button" onClick={() => setEditing((v) => !v)} disabled={!!busy}>
          {editing ? "收起编辑" : "编辑"}
        </button>
        <button className="btn" type="button" onClick={toggle} disabled={!!busy}>
          <Icon name={task.enabled ? "pause" : "play"} size={18} />
          {task.enabled ? "暂停定时" : "恢复定时"}
        </button>
        <button className="btn btn-danger" type="button" onClick={remove} disabled={!!busy}>
          <Icon name="trash" size={18} />
          删除
        </button>
      </div>
      <Message message={msg} />
    </li>
  );
}

function TasksPage() {
  const tasks = useLoad(() => request("/api/tasks"));
  const clouds = useLoad(() => request("/api/clouds"));
  const loading = tasks.loading || clouds.loading;
  const error = tasks.error || clouds.error;

  return (
    <>
      <h1>备份任务</h1>
      {loading && <p role="status">正在加载…</p>}
      {error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{error}</span>
        </p>
      )}
      {tasks.data && clouds.data && (
        <>
          <TaskForm clouds={clouds.data} onSaved={tasks.reload} />
          <h2>已有任务</h2>
          {tasks.data.length === 0 ? (
            <p className="empty">还没有备份任务。</p>
          ) : (
            <ul className="task-list">
              {tasks.data.map((t) => (
                <TaskRow key={t.id} task={t} clouds={clouds.data} onChanged={tasks.reload} />
              ))}
            </ul>
          )}
        </>
      )}
    </>
  );
}

/* ---------- 运行记录 ---------- */

function taskNameMap(tasks) {
  const map = {};
  (tasks || []).forEach((t) => {
    map[t.id] = t.name;
  });
  return map;
}

function RunsPage() {
  const runs = useLoad(() => request("/api/runs"));
  const tasks = useLoad(() => request("/api/tasks"));
  const running = !!runs.data?.some((r) => r.status === "running");
  usePoll(runs.reload, running);

  return (
    <>
      <h1>运行记录</h1>
      {runs.loading && <p role="status">正在加载…</p>}
      {runs.error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{runs.error}</span>
        </p>
      )}
      {running && <p role="status">有任务正在备份，每 3 秒自动刷新。</p>}
      {runs.data && <RunTable runs={runs.data} taskNames={taskNameMap(tasks.data)} />}
    </>
  );
}

// 逐文件结果：状态一律用文字，失败时显示原因。
function FileTable({ files }) {
  if (!files.length) return <p className="empty">暂无文件记录。</p>;
  return (
    <div className="table-wrap">
      <table>
        <caption className="sr-only">文件结果</caption>
        <thead>
          <tr>
            <th scope="col">文件</th>
            <th scope="col">结果</th>
            <th scope="col">错误</th>
          </tr>
        </thead>
        <tbody>
          {files.map((f, i) => (
            <tr key={`${i}-${f.relativePath}`}>
              <td className="cell-wrap">{f.relativePath}</td>
              <td>
                <StatusBadge status={f.status} />
              </td>
              <td className="cell-wrap">{f.status === "failed" ? f.error || "未知错误" : ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RunDetailPage({ id }) {
  const run = useLoad(() => request(`/api/runs/${id}`), String(id));
  const tasks = useLoad(() => request("/api/tasks"));
  const r = run.data;
  usePoll(run.reload, r?.status === "running");
  const taskName = taskNameMap(tasks.data)[r?.taskId] || (r ? `任务 ${r.taskId}` : "");

  return (
    <>
      <p>
        <a href="#/runs">← 返回运行记录</a>
      </p>
      <h1>运行详情 #{id}</h1>
      {run.loading && <p role="status">正在加载…</p>}
      {run.error && (
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{run.error}</span>
        </p>
      )}
      {r && (
        <section className="card">
          <p>
            <StatusBadge status={r.status} />
          </p>
          {r.status === "running" && (
            <p role="status">{r.progressNote ? `${r.progressNote}。每 3 秒自动刷新。` : "正在备份，每 3 秒自动刷新。"}</p>
          )}
          <dl className="meta">
            <div>
              <dt>任务</dt>
              <dd>{taskName}</dd>
            </div>
            <div>
              <dt>触发方式</dt>
              <dd>{TRIGGER_TEXT[r.trigger] || r.trigger}</dd>
            </div>
            <div>
              <dt>开始时间</dt>
              <dd>{formatTime(r.startedAt)}</dd>
            </div>
            <div>
              <dt>结束时间</dt>
              <dd>{formatTime(r.finishedAt)}</dd>
            </div>
            <div>
              <dt>成功文件</dt>
              <dd>{r.successCount}</dd>
            </div>
            <div>
              <dt>跳过文件</dt>
              <dd>{r.skipCount}</dd>
            </div>
            <div>
              <dt>失败文件</dt>
              <dd>{r.failCount}</dd>
            </div>
          </dl>
          {r.errorSummary && (
            <p className="message message-error">
              <Icon name="alert" size={18} />
              <span>{r.errorSummary}</span>
            </p>
          )}
        </section>
      )}
      {r && (
        <section aria-labelledby="run-files-title">
          <h2 id="run-files-title">文件结果</h2>
          <FileTable files={r.files || []} />
        </section>
      )}
    </>
  );
}

/* ---------- 外壳与路由 ---------- */

function parseHash(hash) {
  const parts = hash.replace(/^#\/?/, "").split("/").filter(Boolean);
  if (parts[0] === "clouds") return { page: "clouds" };
  if (parts[0] === "tasks") return { page: "tasks" };
  if (parts[0] === "runs") {
    const id = Number(parts[1]);
    if (parts[1] && Number.isInteger(id) && id > 0) return { page: "run", id };
    return { page: "runs" };
  }
  return { page: "overview" };
}

function useRoute() {
  const [route, setRoute] = useState(() => parseHash(window.location.hash));
  useEffect(() => {
    const onChange = () => setRoute(parseHash(window.location.hash));
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return route;
}

function Shell({ username, onLogout, logoutError }) {
  const route = useRoute();
  const active = route.page === "run" ? "runs" : route.page;
  const mainRef = useRef(null);

  useEffect(() => {
    mainRef.current?.focus({ preventScroll: true });
  }, [route.page, route.id]);

  let content;
  switch (route.page) {
    case "clouds":
      content = <CloudsPage />;
      break;
    case "tasks":
      content = <TasksPage />;
      break;
    case "runs":
      content = <RunsPage />;
      break;
    case "run":
      content = <RunDetailPage id={route.id} />;
      break;
    default:
      content = <OverviewPage />;
  }

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="brand">bak</div>
        <nav aria-label="主导航">
          <ul>
            {NAV.map((n) => (
              <li key={n.page}>
                <a href={n.href} aria-current={active === n.page ? "page" : undefined} className={active === n.page ? "active" : ""}>
                  <Icon name={n.icon} />
                  <span>{n.label}</span>
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <div className="sidebar-foot">
          {username && <p className="who">当前账号：{username}</p>}
          {logoutError && (
            <p className="sidebar-error" role="alert">
              {logoutError}
            </p>
          )}
          <button className="nav-btn" type="button" onClick={onLogout}>
            <Icon name="logout" />
            <span>退出登录</span>
          </button>
        </div>
      </aside>
      <main className="content" tabIndex={-1} ref={mainRef}>
        {content}
      </main>
    </div>
  );
}

export default function App() {
  // phase: loading | setup | login | app | failed
  const [phase, setPhase] = useState("loading");
  const [username, setUsername] = useState("");
  const [bootError, setBootError] = useState("");
  const [logoutError, setLogoutError] = useState("");

  const boot = useCallback(async () => {
    setPhase("loading");
    setBootError("");
    try {
      const status = await request("/api/setup/status", { public: true });
      if (status.needsSetup) {
        setPhase("setup");
        return;
      }
      try {
        await request("/api/me", { public: true });
        setPhase("app");
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) setPhase("login");
        else throw err;
      }
    } catch (err) {
      setBootError(errorText(err));
      setPhase("failed");
    }
  }, []);

  useEffect(() => {
    boot();
  }, [boot]);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setUsername("");
      setPhase((p) => (p === "app" ? "login" : p));
    });
    return () => setUnauthorizedHandler(null);
  }, []);

  const enter = (name) => {
    setUsername(name);
    setLogoutError("");
    setPhase("app");
  };

  async function logout() {
    setLogoutError("");
    try {
      await request("/api/logout", { method: "POST", public: true });
    } catch (err) {
      // 会话已失效也算退出成功；其他错误保持登录并提示。
      if (!(err instanceof ApiError && err.status === 401)) {
        setLogoutError(errorText(err));
        return;
      }
    }
    setUsername("");
    setPhase("login");
  }

  if (phase === "loading") {
    return (
      <main className="auth-page">
        <p role="status">正在加载…</p>
      </main>
    );
  }
  if (phase === "failed") {
    return (
      <AuthShell title="无法加载">
        <p className="message message-error" role="alert">
          <Icon name="alert" size={18} />
          <span>{bootError}</span>
        </p>
        <button className="btn btn-primary" type="button" onClick={boot}>
          重试
        </button>
      </AuthShell>
    );
  }
  if (phase === "setup") return <SetupForm onDone={enter} />;
  if (phase === "login") return <LoginForm onDone={enter} />;
  return <Shell username={username} onLogout={logout} logoutError={logoutError} />;
}
