import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const app = readFileSync(new URL("./App.jsx", import.meta.url), "utf8");
const css = readFileSync(new URL("./styles.css", import.meta.url), "utf8");

function taskFormSource() {
  const start = app.indexOf("function TaskForm");
  const end = app.indexOf("function TaskRow");
  assert.ok(start >= 0 && end > start);
  return app.slice(start, end);
}

function cssBlock(selector) {
  const pattern = new RegExp(`${selector}\\s*\\{([^}]*)\\}`);
  const match = css.match(pattern);
  assert.ok(match, `缺少样式 ${selector}`);
  return match[1];
}

test("新建和编辑任务弹窗把标题、表单、按钮分成三层", () => {
  const form = taskFormSource();
  const head = form.indexOf('className="dialog-sheet-head"');
  const body = form.indexOf('className="dialog-sheet-body"');
  const foot = form.indexOf('className="dialog-sheet-foot"');
  assert.ok(form.includes('className="dialog-sheet"'));
  assert.ok(head >= 0 && body > head && foot > body);

  const fields = form.slice(body, foot);
  assert.match(fields, /任务名称/);
  assert.match(fields, /本地目录/);
  assert.match(fields, /启用定时备份/);
  assert.doesNotMatch(fields, /保存任务/);

  const actions = form.slice(foot);
  assert.match(actions, /保存修改/);
  assert.match(actions, /保存任务/);
  assert.match(actions, /取消/);
  assert.doesNotMatch(actions, /任务名称/);
});

test("只有中间表单区域滚动，标题和按钮不参与收缩", () => {
  assert.match(cssBlock(String.raw`\.dialog:has\(\.dialog-sheet\)`), /overflow:\s*hidden/);
  assert.match(cssBlock(String.raw`\.dialog-sheet`), /overflow:\s*hidden/);
  assert.match(cssBlock(String.raw`\.dialog-sheet`), /max-height:\s*calc\(100dvh - 3rem - 2px\)/);
  assert.match(cssBlock(String.raw`\.dialog-sheet-head,\s*\.dialog-sheet-foot`), /flex:\s*0\s*0\s*auto/);
  assert.match(cssBlock(String.raw`\.dialog-sheet-body`), /overflow-y:\s*auto/);
  assert.match(cssBlock(String.raw`\.dialog-sheet-body`), /min-height:\s*0/);
  assert.match(cssBlock(String.raw`\.dialog-sheet-foot \.btn-row`), /margin-top:\s*0/);
});
