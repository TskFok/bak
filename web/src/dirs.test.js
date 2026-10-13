import assert from "node:assert/strict";
import test from "node:test";
import { dirBaseName, dirsQuery, forgetDir, readRecentDirs, recentDirUnreadable, rememberDir, writeRecentDirs } from "./dirs.js";

test("空路径请求主目录", () => {
  assert.equal(dirsQuery(""), "/api/dirs");
  assert.equal(dirsQuery("   "), "/api/dirs");
  assert.equal(dirsQuery(), "/api/dirs");
});

test("目录路径放进查询参数", () => {
  assert.equal(dirsQuery("/Users/me/My Documents"), "/api/dirs?path=" + encodeURIComponent("/Users/me/My Documents"));
  assert.equal(dirsQuery("/资料/照片"), "/api/dirs?path=" + encodeURIComponent("/资料/照片"));
});

test("记住目录时最新的在前，并去掉重复和空白", () => {
  assert.deepEqual(rememberDir(["/a", "/b"], "/b"), ["/b", "/a"]);
  assert.deepEqual(rememberDir(["/a", "  ", 3], "  /c  "), ["/c", "/a"]);
  assert.deepEqual(rememberDir(["/a"], ""), ["/a"]);
  assert.deepEqual(rememberDir(null, "/a"), ["/a"]);
});

test("最近目录最多保留 8 条", () => {
  const existing = ["/1", "/2", "/3", "/4", "/5", "/6", "/7", "/8"];
  assert.deepEqual(rememberDir(existing, "/0"), ["/0", "/1", "/2", "/3", "/4", "/5", "/6", "/7"]);
});

test("只有目录打不开时才从最近列表删除", () => {
  assert.equal(recentDirUnreadable(400), true);
  assert.equal(recentDirUnreadable(0), false);
  assert.equal(recentDirUnreadable(500), false);
  assert.equal(recentDirUnreadable(undefined), false);
});

test("打不开的最近目录从列表删除", () => {
  assert.deepEqual(forgetDir(["/a", "/b", "/c"], "/b"), ["/a", "/c"]);
  assert.deepEqual(forgetDir(["/a"], "  /missing  "), ["/a"]);
  assert.deepEqual(forgetDir(["/a"], ""), ["/a"]);
});

test("最近目录可以读写本地存储，坏数据当成空列表", () => {
  const data = {};
  const storage = {
    getItem: (key) => (Object.prototype.hasOwnProperty.call(data, key) ? data[key] : null),
    setItem: (key, value) => {
      data[key] = String(value);
    },
  };
  writeRecentDirs(storage, ["/old", "/new", "/old"]);
  assert.deepEqual(readRecentDirs(storage), ["/old", "/new"]);
  storage.setItem("bak.recentDirs", "{");
  assert.deepEqual(readRecentDirs(storage), []);
  storage.setItem("bak.recentDirs", JSON.stringify({ path: "/a" }));
  assert.deepEqual(readRecentDirs(storage), []);
});

test("本地存储写失败时不影响选择目录", () => {
  const storage = {
    setItem() {
      throw new Error("quota");
    },
  };
  assert.doesNotThrow(() => writeRecentDirs(storage, ["/a"]));
});

test("目录名取路径最后一段", () => {
  assert.equal(dirBaseName("/Users/me/My Documents"), "My Documents");
  assert.equal(dirBaseName("/资料/照片"), "照片");
  assert.equal(dirBaseName("/"), "/");
  assert.equal(dirBaseName("C:\\Users\\me\\"), "me");
  assert.equal(dirBaseName(""), "");
});
