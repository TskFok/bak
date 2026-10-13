import assert from "node:assert/strict";
import test from "node:test";
import { PACK_UPLOAD_NOTE, parsePackProgress } from "./packProgress.js";

test("解析压缩进度", () => {
  assert.deepEqual(parsePackProgress("正在压缩 3/120"), { done: 3, total: 120, percent: 3 });
  assert.deepEqual(parsePackProgress("正在压缩 0/120"), { done: 0, total: 120, percent: 0 });
  assert.deepEqual(parsePackProgress("正在压缩 2/3"), { done: 2, total: 3, percent: 67 });
  assert.deepEqual(parsePackProgress("正在压缩 120/120"), { done: 120, total: 120, percent: 100 });
});

test("无法表示成进度条的说明返回空", () => {
  assert.equal(parsePackProgress(PACK_UPLOAD_NOTE), null);
  assert.equal(parsePackProgress(""), null);
  assert.equal(parsePackProgress(null), null);
  assert.equal(parsePackProgress("正在压缩 3/0"), null);
  assert.equal(parsePackProgress("正在压缩 5/3"), null);
  assert.equal(parsePackProgress("正在压缩 3/120。"), null);
});
