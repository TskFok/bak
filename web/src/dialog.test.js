import assert from "node:assert/strict";
import test from "node:test";
import { dialogKeyAction, dialogLayerDepth, isTopDialogLayer, popDialogLayer, pushDialogLayer, resetDialogLayers } from "./dialog.js";

const inside = { activeInside: true };

test("Escape 关闭弹窗", () => {
  assert.deepEqual(dialogKeyAction("Escape", inside), { type: "close" });
});

test("Tab 在最后一个可聚焦元素时回到第一个", () => {
  assert.deepEqual(dialogKeyAction("Tab", { ...inside, activeIsLast: true }), { type: "focus", target: "first" });
});

test("Shift+Tab 在第一个可聚焦元素时回到最后一个", () => {
  assert.deepEqual(dialogKeyAction("Tab", { ...inside, shiftKey: true, activeIsFirst: true }), { type: "focus", target: "last" });
});

test("焦点在弹窗外时 Tab 拉回弹窗", () => {
  assert.deepEqual(dialogKeyAction("Tab", { activeInside: false }), { type: "focus", target: "first" });
  assert.deepEqual(dialogKeyAction("Tab", { shiftKey: true, activeInside: false }), { type: "focus", target: "last" });
});

test("焦点在中间时 Tab 交给浏览器", () => {
  assert.equal(dialogKeyAction("Tab", inside), null);
  assert.equal(dialogKeyAction("Enter", { ...inside, activeIsLast: true }), null);
});

test("后打开的弹窗盖在上面，关掉后露出下一层", () => {
  resetDialogLayers();
  const outer = pushDialogLayer();
  const inner = pushDialogLayer();
  assert.equal(isTopDialogLayer(outer), false);
  assert.equal(isTopDialogLayer(inner), true);
  assert.equal(dialogLayerDepth(outer), 0);
  assert.equal(dialogLayerDepth(inner), 1);
  popDialogLayer(inner);
  assert.equal(isTopDialogLayer(outer), true);
  assert.equal(dialogLayerDepth(inner), 0);
  popDialogLayer(outer);
  assert.equal(isTopDialogLayer(outer), false);
  popDialogLayer(outer);
  assert.equal(isTopDialogLayer(outer), false);
});
