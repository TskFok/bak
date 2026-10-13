const layers = [];
let nextLayerId = 1;

export function pushDialogLayer() {
  const id = nextLayerId;
  nextLayerId += 1;
  layers.push(id);
  return id;
}

export function popDialogLayer(id) {
  const index = layers.lastIndexOf(id);
  if (index >= 0) layers.splice(index, 1);
}

export function isTopDialogLayer(id) {
  return layers.length > 0 && layers[layers.length - 1] === id;
}

export function dialogLayerDepth(id) {
  const index = layers.indexOf(id);
  return index < 0 ? 0 : index;
}

export function resetDialogLayers() {
  layers.length = 0;
  nextLayerId = 1;
}

// 弹窗键盘行为：Escape 关闭；Tab 在两端或焦点跑出弹窗时循环。
export function dialogKeyAction(key, { shiftKey = false, activeInside = false, activeIsFirst = false, activeIsLast = false } = {}) {
  if (key === "Escape") return { type: "close" };
  if (key !== "Tab") return null;
  if (shiftKey && (activeIsFirst || !activeInside)) return { type: "focus", target: "last" };
  if (!shiftKey && (activeIsLast || !activeInside)) return { type: "focus", target: "first" };
  return null;
}
