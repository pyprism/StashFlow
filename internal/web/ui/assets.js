const state = {
  items: [],
  storage: null,
  activeId: null,
};

let isDragging = false;

const queueList = document.getElementById("queueList");
const completedList = document.getElementById("completedList");
const storageStats = document.getElementById("storageStats");

const magnetInput = document.getElementById("magnetInput");
const fileInput = document.getElementById("fileInput");
const addMessage = document.getElementById("addMessage");

const storagePath = document.getElementById("storagePath");
const portInput = document.getElementById("portInput");
const maxUsageInput = document.getElementById("maxUsageInput");
const settingsNote = document.getElementById("settingsNote");

const storageNote = document.getElementById("storageNote");

const themeToggle = document.getElementById("themeToggle");

function formatBytes(bytes) {
  if (!bytes && bytes !== 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let idx = 0;
  let val = bytes;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx++;
  }
  return `${val.toFixed(1)} ${units[idx]}`;
}

function formatPercent(value) {
  if (value === null || value === undefined) return "0%";
  return `${(value * 100).toFixed(1)}%`;
}

function showToast(el, text, type, timeout = 4000) {
  el.textContent = text;
  el.className = "toast" + (type ? ` ${type}` : "");
  el.style.display = "block";
  if (timeout) {
    setTimeout(() => {
      el.style.display = "none";
    }, timeout);
  }
}

function escapeHtml(str) {
  const div = document.createElement("div");
  div.textContent = str;
  return div.innerHTML;
}

function renderStorage() {
  if (!state.storage) {
    storageStats.innerHTML = '<div class="empty">No storage data available.</div>';
    return;
  }
  storageStats.innerHTML = "";
  const rows = [
    ["Total", formatBytes(state.storage.total_bytes)],
    ["Used", formatBytes(state.storage.used_bytes)],
    ["Free", formatBytes(state.storage.free_bytes)],
    ["Max Allowed", `${formatBytes(state.storage.max_usage_bytes)} (${formatPercent(state.storage.max_usage_pct)})`],
    ["Available for New", formatBytes(state.storage.available_for_new)],
  ];
  rows.forEach(([label, value]) => {
    const row = document.createElement("div");
    row.className = "stat";
    row.innerHTML = `<span>${escapeHtml(label)}</span><span>${escapeHtml(value)}</span>`;
    storageStats.appendChild(row);
  });
}

function buildTorrentCard(item, isCompleted = false) {
  const card = document.createElement("div");
  card.className = "torrent";
  card.draggable = !isCompleted;
  card.dataset.id = item.id;

  const title = item.name || "(Fetching metadata\u2026)";
  const size = item.size_bytes ? formatBytes(item.size_bytes) : "-";
  const progress = isCompleted ? 1 : (item.progress || 0);

  const meta = document.createElement("div");
  meta.className = "meta";

  let downloadInfo = "";
  if (item.status === "downloading" && item.size_bytes) {
    downloadInfo = `<span class="download-info">${formatBytes(item.downloaded || 0)} / ${formatBytes(item.size_bytes)}</span>`;
  }

  meta.innerHTML = `
    <strong title="${escapeHtml(title)}">${escapeHtml(title)}</strong>
    <span>${escapeHtml(size)}</span>
    <div class="progress"><span style="width:${(progress * 100).toFixed(1)}%"></span></div>
    ${downloadInfo}
    <span class="status ${item.status}">${escapeHtml(item.status)}</span>
  `;

  if (item.error_message) {
    const errorEl = document.createElement("span");
    errorEl.className = "error-msg";
    errorEl.textContent = item.error_message;
    meta.appendChild(errorEl);
  }

  const actions = document.createElement("div");
  const removeBtn = document.createElement("button");
  removeBtn.className = "danger";
  removeBtn.textContent = "Remove";
  removeBtn.onclick = () => removeTorrent(item.id, item.name || item.id);
  actions.appendChild(removeBtn);

  card.appendChild(meta);
  card.appendChild(actions);

  return card;
}

function renderQueues() {
  queueList.innerHTML = "";
  completedList.innerHTML = "";

  const queued = state.items.filter(item => item.status !== "completed");
  const completed = state.items.filter(item => item.status === "completed");

  document.getElementById("queueCount").textContent = queued.length;
  document.getElementById("completedCount").textContent = completed.length;

  if (queued.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "No torrents in queue. Add a magnet link or torrent file above.";
    queueList.appendChild(empty);
  } else {
    queued.forEach(item => {
      const card = buildTorrentCard(item, false);
      queueList.appendChild(card);
    });
  }

  if (completed.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "No completed downloads yet.";
    completedList.appendChild(empty);
  } else {
    completed.forEach(item => {
      const card = buildTorrentCard(item, true);
      completedList.appendChild(card);
    });
  }

  enableDragAndDrop();
}

function updateState(payload) {
  if (payload.state) {
    state.items = payload.state.items || [];
    state.activeId = payload.state.active_id;
  }
  if (payload.storage) {
    state.storage = payload.storage;
  }
  renderStorage();
  if (!isDragging) {
    renderQueues();
  }
}

async function loadSettings() {
  try {
    const res = await fetch("/api/settings");
    if (!res.ok) return;
    const data = await res.json();
    storagePath.value = data.storage_path || "";
    portInput.value = data.port || 0;
    maxUsageInput.value = data.max_usage_percent
      ? Math.round(data.max_usage_percent * 100)
      : 90;
  } catch (e) {
    console.error("Failed to load settings:", e);
  }
}

async function saveSettings() {
  const btn = document.getElementById("saveSettings");
  const origText = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Saving\u2026";

  try {
    const maxPct = Number(maxUsageInput.value);
    if (maxPct < 1 || maxPct > 100 || isNaN(maxPct)) {
      showToast(settingsNote, "Max usage must be between 1 and 100.", "error");
      return;
    }

    const payload = {
      storage_path: storagePath.value,
      port: Number(portInput.value),
      max_usage_percent: maxPct / 100,
    };
    const res = await fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!res.ok) {
      showToast(settingsNote, data.error || "Failed to save settings", "error");
      return;
    }
    if (data.restartRequired) {
      showToast(settingsNote, "Settings saved. Restart required to apply port or storage changes.", "success");
    } else {
      showToast(settingsNote, "Settings saved.", "success");
    }
  } catch (e) {
    showToast(settingsNote, "Network error saving settings.", "error");
  } finally {
    btn.disabled = false;
    btn.textContent = origText;
  }
}

async function addTorrent() {
  addMessage.style.display = "none";
  const btn = document.getElementById("addTorrent");
  const origText = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Adding\u2026";

  try {
    const form = new FormData();
    if (magnetInput.value.trim() !== "") {
      form.append("magnet", magnetInput.value.trim());
    } else if (fileInput.files.length > 0) {
      form.append("file", fileInput.files[0]);
    } else {
      showToast(addMessage, "Please provide a magnet link or torrent file.", "error");
      return;
    }

    const res = await fetch("/api/torrents", {
      method: "POST",
      body: form,
    });
    const data = await res.json();
    if (!res.ok) {
      showToast(addMessage, data.error || "Failed to add torrent", "error");
      return;
    }
    magnetInput.value = "";
    fileInput.value = "";
    showToast(addMessage, "Torrent queued successfully.", "success");
  } catch (e) {
    showToast(addMessage, "Network error adding torrent.", "error");
  } finally {
    btn.disabled = false;
    btn.textContent = origText;
  }
}

async function removeTorrent(id, name) {
  if (!confirm(`Remove "${name || id}" from the queue?`)) return;
  try {
    await fetch(`/api/torrents/${id}`, { method: "DELETE" });
  } catch (e) {
    console.error("Failed to remove torrent:", e);
  }
}

async function manualCheck() {
  const btn = document.getElementById("manualCheck");
  const origText = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Checking\u2026";

  try {
    const res = await fetch("/api/storage/check", { method: "POST" });
    if (!res.ok) {
      showToast(storageNote, "Storage check failed.", "error");
      return;
    }
    const data = await res.json();
    state.storage = data;
    renderStorage();
    showToast(storageNote, "Storage check complete.", "success");
  } catch (e) {
    showToast(storageNote, "Network error during storage check.", "error");
  } finally {
    btn.disabled = false;
    btn.textContent = origText;
  }
}

function enableDragAndDrop() {
  let dragged = null;
  let didReorder = false;

  queueList.querySelectorAll(".torrent").forEach((card) => {
    card.addEventListener("dragstart", (e) => {
      dragged = card;
      didReorder = false;
      isDragging = true;
      card.classList.add("dragging");
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", card.dataset.id);
    });

    card.addEventListener("dragend", () => {
      card.classList.remove("dragging");
      queueList.querySelectorAll(".torrent").forEach(c => c.classList.remove("drag-over"));
      isDragging = false;
      if (didReorder) {
        // Update local state.items order to match the DOM so the next
        // SSE-driven render doesn't snap back to the old order.
        const domOrder = Array.from(queueList.querySelectorAll(".torrent")).map(el => el.dataset.id);
        const byId = {};
        state.items.forEach(item => { byId[item.id] = item; });
        const completed = state.items.filter(item => item.status === "completed");
        const reordered = domOrder.filter(id => byId[id]).map(id => byId[id]);
        state.items = reordered.concat(completed);
        sendOrder();
      }
      dragged = null;
      didReorder = false;
    });

    card.addEventListener("dragover", (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      if (!dragged || dragged === card) return;
      queueList.querySelectorAll(".torrent").forEach(c => {
        if (c !== card) c.classList.remove("drag-over");
      });
      card.classList.add("drag-over");
      const rect = card.getBoundingClientRect();
      const after = (e.clientY - rect.top) / rect.height > 0.5;
      if (after) {
        if (card.nextSibling !== dragged) {
          queueList.insertBefore(dragged, card.nextSibling);
          didReorder = true;
        }
      } else {
        if (card.previousSibling !== dragged) {
          queueList.insertBefore(dragged, card);
          didReorder = true;
        }
      }
    });

    card.addEventListener("dragleave", () => {
      card.classList.remove("drag-over");
    });

    card.addEventListener("drop", (e) => {
      e.preventDefault();
      card.classList.remove("drag-over");
    });
  });
}

// One-time container-level listeners (prevents accumulation from enableDragAndDrop)
queueList.addEventListener("dragover", (e) => {
  e.preventDefault();
  e.dataTransfer.dropEffect = "move";
});
queueList.addEventListener("drop", (e) => {
  e.preventDefault();
});

async function sendOrder() {
  const order = Array.from(queueList.querySelectorAll(".torrent")).map(el => el.dataset.id);
  try {
    await fetch("/api/queue/reorder", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ order }),
    });
  } catch (e) {
    console.error("Failed to reorder:", e);
  }
}

function initTheme() {
  const stored = localStorage.getItem("stashflow-theme");
  if (stored) {
    document.documentElement.setAttribute("data-theme", stored);
    themeToggle.checked = stored === "dark";
  } else {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    document.documentElement.setAttribute("data-theme", prefersDark ? "dark" : "light");
    themeToggle.checked = prefersDark;
  }

  themeToggle.addEventListener("change", () => {
    const theme = themeToggle.checked ? "dark" : "light";
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("stashflow-theme", theme);
  });
}

function connectEvents() {
  const source = new EventSource("/api/events");
  source.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      updateState(data);
    } catch (e) {
      console.error("SSE parse error:", e);
    }
  };
  source.onerror = () => {
    console.warn("SSE connection lost, will auto-reconnect\u2026");
  };
}

async function init() {
  initTheme();
  await loadSettings();
  try {
    const res = await fetch("/api/state");
    if (res.ok) {
      const data = await res.json();
      updateState(data);
    }
  } catch (e) {
    console.error("Failed to load initial state:", e);
  }
  connectEvents();
}


document.getElementById("addTorrent").addEventListener("click", addTorrent);
document.getElementById("saveSettings").addEventListener("click", saveSettings);
document.getElementById("manualCheck").addEventListener("click", manualCheck);

init();
