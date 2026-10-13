import assert from "node:assert/strict";
import test from "node:test";
import { dirsQuery } from "./dirs.js";

test("空路径请求主目录", () => {
  assert.equal(dirsQuery(""), "/api/dirs");
  assert.equal(dirsQuery("   "), "/api/dirs");
  assert.equal(dirsQuery(), "/api/dirs");
});

test("目录路径放进查询参数", () => {
  assert.equal(dirsQuery("/Users/me/My Documents"), "/api/dirs?path=" + encodeURIComponent("/Users/me/My Documents"));
  assert.equal(dirsQuery("/资料/照片"), "/api/dirs?path=" + encodeURIComponent("/资料/照片"));
});
