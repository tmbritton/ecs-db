// Playback is client-owned presentation. The server describes ordered column
// indices; the renderer still owns the game, not this editor's preview timer.
const started = new WeakMap();
let running = false;

function tick(now) {
  const previews = document.querySelectorAll('[data-testid="sprite-playback"]');
  if (!previews.length) {
    running = false;
    return;
  }
  for (const node of previews) {
    const signature = JSON.stringify([node.dataset.sheet, node.dataset.frames, node.dataset.fps, node.dataset.loop, node.dataset.tileSize]);
    let state = started.get(node);
    if (!state || state.signature !== signature) {
      state = { start: now, signature };
      started.set(node, state);
    }
    const frames = node.dataset.frames.split(",").map((s) => Number(s.trim()));
    const elapsed = Math.floor(((now - state.start) / 1000) * Number(node.dataset.fps));
    const index = node.dataset.loop === "true" ? elapsed % frames.length : Math.min(elapsed, frames.length - 1);
    node.style.backgroundPositionX = `${-frames[index] * Number(node.dataset.tileSize) * 4}px`;
  }
  requestAnimationFrame(tick);
}

function start() {
  if (!running && document.querySelector('[data-testid="sprite-playback"]')) {
    running = true;
    requestAnimationFrame(tick);
  }
}

new MutationObserver(start).observe(document, { childList: true, subtree: true });
start();
