const state = {
  containers: [],
  forwards: [],
  modalTarget: null,
};

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok) {
    let body = {};
    try { body = await res.json(); } catch (_) {}
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  if (res.status === 204) return null;
  return res.json();
}

function showToast(msg, type = 'success') {
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = `toast toast-${type}`;
  setTimeout(() => el.classList.add('hidden'), 3000);
}

function showEl(id) { document.getElementById(id).classList.remove('hidden'); }
function hideEl(id) { document.getElementById(id).classList.add('hidden'); }

async function refreshContainers() {
  hideEl('containers-empty');
  hideEl('containers-error');
  hideEl('containers-table');
  showEl('containers-loading');

  try {
    state.containers = await api('/api/containers');
  } catch (err) {
    hideEl('containers-loading');
    showEl('containers-error');
    document.getElementById('containers-error-msg').textContent =
      `Failed to load containers: ${err.message}`;
    return;
  }

  hideEl('containers-loading');
  if (!state.containers.length) {
    showEl('containers-empty');
    return;
  }
  renderContainers();
  showEl('containers-table');
}

function renderContainers() {
  const tbody = document.getElementById('containers-tbody');
  tbody.innerHTML = state.containers.map((c, idx) => {
    const stateClass = c.state === 'running' ? 'running' : 'stopped';

    const ips = Object.entries(c.networkIps || {})
      .map(([net, ip]) => `<div class="ip" title="${net}">${escapeHtml(ip)}</div>`)
      .join('') || '<span style="color:#94a3b8">&mdash;</span>';

    const ports = (c.ports || []).map(p => {
      const label = p.hostPort
        ? `${p.hostPort}\u2192${p.port}/${p.protocol}`
        : `${p.port}/${p.protocol}`;
      const cls = p.hostPort ? 'mapped' : 'unmapped';
      return `<span class="port-tag ${cls}">${escapeHtml(label)}</span>`;
    }).join('') || '<span style="color:#94a3b8">&mdash;</span>';

    const forwardBtn = c.state === 'running'
      ? `<button class="btn btn-primary btn-sm" data-action="forward" data-idx="${idx}">Forward</button>`
      : '';

    return `<tr>
      <td><strong>${escapeHtml(c.name)}</strong><br><span style="font-size:0.7rem;color:#94a3b8">${escapeHtml(c.id)}</span></td>
      <td>${escapeHtml(c.image)}</td>
      <td><span class="state state-${stateClass}">${escapeHtml(c.state)}</span></td>
      <td>${ips}</td>
      <td>${ports}</td>
      <td>${forwardBtn}</td>
    </tr>`;
  }).join('');
}

function openForwardModal(idx) {
  const c = state.containers[idx];
  if (!c) return;

  state.modalTarget = c;
  document.getElementById('modal-container').value = c.name;
  document.getElementById('modal-ip').value = Object.values(c.networkIps || {})[0] || 'unknown';

  const select = document.getElementById('modal-container-port');
  select.innerHTML = '<option value="">-- Select port --</option>';
  c.ports.forEach(p => {
    const label = p.hostPort
      ? `${p.port}/${p.protocol} (mapped to :${p.hostPort})`
      : `${p.port}/${p.protocol}`;
    select.innerHTML += `<option value="${p.port}">${escapeHtml(label)}</option>`;
  });

  document.getElementById('modal-local-port').value = '';
  showEl('modal');
}

function syncLocalPort() {
  const port = document.getElementById('modal-container-port').value;
  if (port && !document.getElementById('modal-local-port').value) {
    document.getElementById('modal-local-port').value = port;
  }
}

function closeModal() {
  hideEl('modal');
  state.modalTarget = null;
}

async function createForward() {
  const c = state.modalTarget;
  if (!c) return;

  const containerPort = parseInt(document.getElementById('modal-container-port').value);
  const localPort = parseInt(document.getElementById('modal-local-port').value) || containerPort;

  if (!containerPort) {
    showToast('Please select a container port', 'error');
    return;
  }

  try {
    await api('/api/forwards', {
      method: 'POST',
      body: JSON.stringify({
        containerId: c.id,
        containerPort,
        localPort,
      }),
    });
    closeModal();
    showToast(`Forward: localhost:${localPort} \u2192 ${c.name}:${containerPort}`);
    refreshForwards();
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

async function refreshForwards() {
  try {
    state.forwards = await api('/api/forwards');
  } catch (_) {
    return;
  }

  hideEl('forwards-loading');
  if (!state.forwards.length) {
    showEl('forwards-empty');
    hideEl('forwards-list');
  } else {
    hideEl('forwards-empty');
    showEl('forwards-list');
    renderForwards();
  }
  document.getElementById('forwards-count').textContent = state.forwards.length;
}

function renderForwards() {
  const list = document.getElementById('forwards-list');
  list.innerHTML = state.forwards.map(f => `
    <div class="forward-card">
      <div>
        <span style="font-weight:600;font-family:monospace">localhost:${f.localPort}</span>
        <span class="arrow"> \u2192 </span>
        <span style="font-weight:600">${escapeHtml(f.containerName)}:${f.containerPort}</span>
      </div>
      <div class="meta">
        IP: ${escapeHtml(f.containerIP)} | ID: ${escapeHtml(f.id)}
      </div>
      <button class="btn btn-danger" style="margin-top:8px;width:100%" data-action="stop-forward" data-id="${escapeHtml(f.id)}">Stop</button>
    </div>
  `).join('');
}

async function stopForward(id) {
  try {
    await api(`/api/forwards/${encodeURIComponent(id)}`, { method: 'DELETE' });
    showToast('Forward stopped');
    refreshForwards();
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-action]');
  if (!btn) return;

  const action = btn.dataset.action;
  if (action === 'forward') {
    const idx = parseInt(btn.dataset.idx);
    openForwardModal(idx);
  } else if (action === 'stop-forward') {
    stopForward(btn.dataset.id);
  }
});

document.getElementById('modal').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closeModal();
});

refreshContainers();
refreshForwards();
setInterval(refreshForwards, 3000);
