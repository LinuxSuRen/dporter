const state = {
  containers: [],
  forwards: [],
  modalTarget: null,
  showAll: false,
  composeFilter: [],
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
  const search = (document.getElementById('containers-search').value || '').toLowerCase();
  const filtered = state.containers.filter(c => {
    if (!state.showAll && c.state !== 'running') return false;
    if (state.composeFilter.length && !state.composeFilter.includes(c.composeProject || '')) return false;
    if (!search) return true;
    return c.name.toLowerCase().includes(search)
      || c.id.toLowerCase().includes(search)
      || c.image.toLowerCase().includes(search);
  });

  renderComposeTags(filtered.length);

  const tbody = document.getElementById('containers-tbody');
  if (filtered.length === 0 && search) {
    tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:#94a3b8;padding:32px">No containers matching &ldquo;${escapeHtml(search)}&rdquo;</td></tr>`;
    return;
  }
  tbody.innerHTML = filtered.map((c) => {
    const realIdx = state.containers.indexOf(c);
    const stateClass = c.state === 'running' ? 'running' : 'stopped';

    const ips = Object.entries(c.networkIps || {})
      .map(([net, ip]) => `<div class="ip" title="${net}">${escapeHtml(ip)}</div>`)
      .join('') || '<span style="color:#94a3b8">&mdash;</span>';

    const ports = (c.ports || []).map(p => {
      if (p.hostPort) {
        const url = `http://${location.hostname}:${p.hostPort}`;
        const label = `${p.hostPort}:${p.port}/${p.protocol}`;
        return `<a href="${url}" target="_blank" rel="noopener" class="port-link" title="Open ${url}">${escapeHtml(label)}</a>`;
      }
      return `<span class="port-tag unmapped">${escapeHtml(`${p.port}/${p.protocol}`)}</span>`;
    }).join('') || '<span style="color:#94a3b8">&mdash;</span>';

    const forwardBtn = c.state === 'running'
      ? `<button type="button" class="btn btn-primary btn-sm" data-action="forward" data-idx="${realIdx}">Forward</button>`
      : '';

    const moreItems = [
      `<button type="button" class="btn-icon" data-action="inspect" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}">Inspect</button>`,
      `<button type="button" class="btn-icon" data-action="pull" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}" data-image="${escapeHtml(c.image)}">Pull Image</button>`,
    ];
    if (c.state === 'running') {
      moreItems.push(
        `<button type="button" class="btn-icon" data-action="logs" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}">Logs</button>`,
        `<button type="button" class="btn-icon" data-action="restart" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}" style="color:#d97706">Restart</button>`,
        `<button type="button" class="btn-icon" data-action="stop-container" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}" style="color:#dc2626">Stop</button>`,
      );
    } else {
      moreItems.push(
        `<button type="button" class="btn-icon" data-action="start-container" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}" style="color:#059669">Start</button>`,
      );
    }
    const moreMenu = `
      <span style="position:relative">
        <button type="button" class="btn-more" data-action="toggle-menu" data-idx="${realIdx}">&hellip;</button>
        <div class="action-dropdown hidden" data-menu-idx="${realIdx}">
          ${moreItems.join('')}
        </div>
      </span>
    `;

    const composeBadge = c.composeProject
      ? `<span class="compose-badge" title="${escapeHtml(c.composeConfigFiles || '')}">${escapeHtml(c.composeProject)}</span>`
      : '';

    return `<tr>
      <td><strong>${escapeHtml(c.name)}</strong>${composeBadge}<br><span style="font-size:0.7rem;color:#94a3b8">${escapeHtml(c.id)}</span></td>
      <td><a href="#" class="image-link" data-action="image-info" data-image="${escapeHtml(c.image)}" title="Click for details">${escapeHtml(c.image)}</a></td>
      <td><span class="state state-${stateClass}">${escapeHtml(c.state)}</span></td>
      <td>${ips}</td>
      <td>${ports}</td>
      <td><div class="action-btns">${forwardBtn}${moreMenu}</div></td>
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
    showToast(`Forward: localhost:${localPort} → ${c.name}:${containerPort}`);
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
  renderForwardsUI();
}

function renderForwardsUI() {
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
  autoCollapseForwards();
}

function autoCollapseForwards() {
  const sec = document.getElementById('forwards-section');
  if (state.forwards.length === 0) {
    sec.classList.add('card-collapsed');
  } else {
    sec.classList.remove('card-collapsed');
  }
}

function toggleForwards() {
  document.getElementById('forwards-section').classList.toggle('card-collapsed');
}

function renderForwards() {
  const list = document.getElementById('forwards-list');
  list.innerHTML = state.forwards.map(f => {
    const url = `http://${location.hostname}:${f.localPort}`;
    return `
    <div class="forward-card">
      <div>
        <a href="${url}" target="_blank" rel="noopener" style="font-weight:600;font-family:monospace;color:#4a6cf7;text-decoration:none" title="Open ${url}">localhost:${f.localPort}</a>
        <span class="arrow"> → </span>
        <span style="font-weight:600">${escapeHtml(f.containerName)}:${f.containerPort}</span>
      </div>
      <div class="meta">
        IP: ${escapeHtml(f.containerIP)} | ID: ${escapeHtml(f.id)}
      </div>
      <button type="button" class="btn btn-danger" style="margin-top:8px;width:100%" data-action="stop-forward" data-id="${escapeHtml(f.id)}">Stop</button>
    </div>
  `}).join('');
}

async function stopForward(id) {
  try {
    await api(`/api/forwards/${encodeURIComponent(id)}`, { method: 'DELETE' });
    showToast('Forward stopped');
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

let logsWs = null;
let logsPaused = false;

function openLogs(containerId, containerName) {
  closeLogs();

  document.getElementById('logs-title').textContent = `Logs: ${containerName}`;
  document.getElementById('logs-pause').textContent = 'Pause';
  document.getElementById('logs-pause').classList.remove('active');
  logsPaused = false;

  const output = document.getElementById('logs-output');
  output.innerHTML = '<div class="logs-placeholder">Connecting...</div>';

  document.getElementById('logs-drawer').classList.add('open');

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/api/containers/${encodeURIComponent(containerId)}/logs`;

  logsWs = new WebSocket(url);

  logsWs.onopen = () => {
    output.innerHTML = '';
  };

  logsWs.onmessage = (e) => {
    if (logsPaused) return;
    const line = document.createElement('div');
    line.textContent = e.data;
    output.appendChild(line);
    output.scrollTop = output.scrollHeight;
  };

  logsWs.onerror = () => {
    output.innerHTML = '<div class="logs-placeholder" style="color:#f85149">Connection error</div>';
  };

  logsWs.onclose = () => {
    if (output.children.length === 0 || output.querySelector('.logs-placeholder')) {
      output.innerHTML = '<div class="logs-placeholder">Disconnected</div>';
    }
  };
}

function closeLogs() {
  if (logsWs) {
    logsWs.close();
    logsWs = null;
  }
  document.getElementById('logs-drawer').classList.remove('open');
}

function toggleLogPause() {
  logsPaused = !logsPaused;
  const btn = document.getElementById('logs-pause');
  if (logsPaused) {
    btn.textContent = 'Resume';
    btn.classList.add('active');
  } else {
    btn.textContent = 'Pause';
    btn.classList.remove('active');
  }
}

function clearLogs() {
  document.getElementById('logs-output').innerHTML = '';
}

async function openInspect(containerId, containerName) {
  document.getElementById('inspect-content').classList.add('hidden');
  document.getElementById('inspect-loading').classList.remove('hidden');
  document.getElementById('inspect-modal').classList.remove('hidden');

  try {
    const data = await api(`/api/containers/${encodeURIComponent(containerId)}/inspect`);
    const el = document.getElementById('inspect-content');
    el.textContent = JSON.stringify(data, null, 2);
    el.classList.remove('hidden');
    document.getElementById('inspect-loading').classList.add('hidden');
  } catch (err) {
    document.getElementById('inspect-loading').textContent = `Failed: ${err.message}`;
  }
}

function closeInspect() {
  document.getElementById('inspect-modal').classList.add('hidden');
}

async function showImageInfo(imageName) {
  document.getElementById('inspect-content').classList.add('hidden');
  document.getElementById('inspect-loading').classList.remove('hidden');
  document.getElementById('inspect-loading').textContent = 'Loading image info...';
  document.getElementById('inspect-modal').classList.remove('hidden');

  try {
    const data = await api(`/api/images/info?name=${encodeURIComponent(imageName)}`);
    const size = data.Size ? `${(data.Size / 1024 / 1024).toFixed(1)} MB` : 'unknown';
    const el = document.getElementById('inspect-content');
    el.textContent = JSON.stringify({
      repos: data.RepoTags,
      size: size,
      created: data.Created,
      arch: `${data.Os}/${data.Architecture}`,
      ...data,
    }, null, 2);
    el.classList.remove('hidden');
    document.getElementById('inspect-loading').classList.add('hidden');
  } catch (err) {
    document.getElementById('inspect-loading').textContent = `Failed: ${err.message}`;
  }
}

async function restartContainer(containerId, containerName) {
  try {
    await api(`/api/containers/${encodeURIComponent(containerId)}/restart`, { method: 'POST' });
    showToast(`Restarting ${containerName}...`);
    refreshContainers();
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

async function stopContainer(containerId, containerName) {
  if (!confirm(`Stop container "${containerName}"?`)) return;
  try {
    await api(`/api/containers/${encodeURIComponent(containerId)}/stop`, { method: 'POST' });
    showToast(`Stopped ${containerName}`);
    refreshContainers();
    refreshForwards();
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

async function startContainer(containerId, containerName) {
  try {
    await api(`/api/containers/${encodeURIComponent(containerId)}/start`, { method: 'POST' });
    showToast(`Started ${containerName}`);
    refreshContainers();
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

let pullEventSource = null;

function startPullStream(url, title) {
  closePull();
  document.getElementById('pull-title').textContent = title;
  document.getElementById('pull-status').textContent = 'Connecting...';
  document.getElementById('pull-layers').innerHTML = '';
  document.getElementById('pull-modal').classList.remove('hidden');

  pullEventSource = new EventSource(url);
  const layers = {};

  pullEventSource.addEventListener('pull-error', (e) => {
    document.getElementById('pull-status').textContent = `Error: ${e.data || 'connection failed'}`;
    pullEventSource.close();
  });

  pullEventSource.addEventListener('done', () => {
    Object.values(layers).forEach(l => { l.current = 1; l.total = 1; });
    renderPullLayers(layers);
    document.getElementById('pull-status').textContent = 'Pull complete';
    pullEventSource.close();
  });

  pullEventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (msg.error) {
        document.getElementById('pull-status').textContent = `Error: ${msg.error}`;
        return;
      }
      if (msg.status) {
        document.getElementById('pull-status').textContent = msg.status;
        if (msg.status.startsWith('Resolved: ') || msg.status.startsWith('Pulling ')) {
          document.getElementById('pull-title').textContent = `Pull: ${msg.status.replace(/^Resolved:\s*/, '').replace(/^Pulling\s+\S+:\s*/, '')}`;
        }
      }
      if (msg.id) {
        const isTag = /pulling from/i.test(msg.status || '');
        if (!isTag) {
          const layer = layers[msg.id] || (layers[msg.id] = {});
          if (msg.progressDetail) {
            layer.current = msg.progressDetail.current || 0;
            layer.total = msg.progressDetail.total || layer.total || 1;
          }
          if (/complete|exists/i.test(msg.status || '')) {
            layer.current = layer.total || 1;
          }
          renderPullLayers(layers);
        }
      }
    } catch (_) {}
  };

  pullEventSource.onerror = () => {
    if (pullEventSource.readyState === EventSource.CLOSED) {
      document.getElementById('pull-status').textContent = 'Disconnected';
    }
  };
}

function openPull(containerId, containerName, image) {
  startPullStream(
    `/api/containers/${encodeURIComponent(containerId)}/pull?image=${encodeURIComponent(image)}`,
    `Pull: ${image}`
  );
}

function renderPullLayers(layers) {
  const el = document.getElementById('pull-layers');
  el.innerHTML = Object.entries(layers).map(([id, l]) => {
    const pct = l.total ? Math.round((l.current / l.total) * 100) : 0;
    const done = pct >= 100;
    const shortId = id.length > 12 ? id.substring(7, 19) : id;
    return `<div class="pull-layer">
      <span class="layer-id">${shortId}</span>
      <div class="layer-bar"><div class="layer-bar-fill${done ? ' done' : ''}" style="width:${pct}%"></div></div>
      <span class="layer-pct">${pct}%</span>
    </div>`;
  }).join('');
}

function closePull() {
  if (pullEventSource) {
    pullEventSource.close();
    pullEventSource = null;
  }
  document.getElementById('pull-modal').classList.add('hidden');
}

function renderComposeTags(filteredCount) {
  const projects = [...new Set(state.containers.filter(c => c.composeProject).map(c => c.composeProject))];
  const el = document.getElementById('compose-tags');
  if (projects.length === 0) {
    el.innerHTML = '';
    el.classList.add('hidden');
    return;
  }
  el.classList.remove('hidden');
  el.innerHTML = `<span class="compose-count">${filteredCount}/${state.containers.length}</span>` + projects.map(p => {
    const active = state.composeFilter.includes(p) ? ' active' : '';
    return `<span class="compose-tag${active}" onclick="filterByCompose('${escapeHtml(p)}')">${escapeHtml(p)}</span>`;
  }).join('');
  if (state.composeFilter.length) {
    el.innerHTML += `<span class="compose-tag clear" onclick="filterByCompose(null)">&times; clear</span>`;
    el.innerHTML += `<span class="compose-action" onclick="composeRestartAll()" title="Restart all containers">&#8635; Restart</span>`;
    el.innerHTML += `<span class="compose-action" onclick="composePullAll()" title="Pull all images">&#8681; Pull</span>`;
  }
}

function filterByCompose(project) {
  if (!project) {
    state.composeFilter = [];
  } else {
    const idx = state.composeFilter.indexOf(project);
    if (idx === -1) {
      state.composeFilter.push(project);
    } else {
      state.composeFilter.splice(idx, 1);
    }
  }
  localStorage.setItem('composeFilter', JSON.stringify(state.composeFilter));
  renderContainers();
}

async function composeRestartAll() {
  const project = state.composeFilter[0];
  if (!project) return;
  if (!confirm(`Restart all containers in "${project}"?`)) return;
  try {
    const res = await api(`/api/compose/restart?project=${encodeURIComponent(project)}`, { method: 'POST' });
    showToast(res.output || 'Restarting...');
    setTimeout(refreshContainers, 2000);
  } catch (err) {
    showToast(`Failed: ${err.message}`, 'error');
  }
}

function composePullAll() {
  const project = state.composeFilter[0];
  if (!project) return;

  closePull();
  document.getElementById('pull-title').textContent = `Pull: ${project} (all)`;
  document.getElementById('pull-status').textContent = 'Connecting...';
  document.getElementById('pull-layers').innerHTML = '';
  document.getElementById('pull-modal').classList.remove('hidden');

  const url = `/api/compose/pull?project=${encodeURIComponent(project)}`;
  pullEventSource = new EventSource(url);

  const containers = {};

  pullEventSource.addEventListener('pull-error', (e) => {
    document.getElementById('pull-status').textContent = `Error: ${e.data || 'failed'}`;
    pullEventSource.close();
  });

  pullEventSource.addEventListener('done', () => {
    document.getElementById('pull-status').textContent = `Pull complete for ${project}`;
    showToast(`Pulled all images for ${project}`);
    pullEventSource.close();
  });

  pullEventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (msg.type === 'container') {
        containers[msg.idx] = msg;
        renderComposePullList(containers);
        if (msg.status === 'pulling') {
          document.getElementById('pull-status').textContent = `Pulling ${msg.name}: ${msg.image}`;
        }
      }
    } catch (_) {}
  };

  pullEventSource.onerror = () => {
    if (pullEventSource.readyState === EventSource.CLOSED) {
      document.getElementById('pull-status').textContent = 'Disconnected';
    }
  };
}

function renderComposePullList(containers) {
  const el = document.getElementById('pull-layers');
  el.innerHTML = Object.values(containers).map(c => {
    let icon, cls;
    switch (c.status) {
      case 'pulling': icon = '⬇'; cls = 'color:#4a6cf7'; break;
      case 'done': icon = '✓'; cls = 'color:#059669'; break;
      case 'error': icon = '✗'; cls = 'color:#dc2626'; break;
      default: icon = '⏳'; cls = 'color:#94a3b8'; break;
    }
    return `<div class="pull-layer">
      <span style="${cls};width:16px;flex-shrink:0">${icon}</span>
      <span style="flex:1;font-family:monospace;font-size:0.75rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(c.name)}</span>
      <span style="color:#94a3b8;font-size:0.7rem;flex-shrink:0">${escapeHtml(c.image ? c.image.substring(0, 40) : '')}</span>
    </div>`;
  }).join('');
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function toggleShowAll() {
  state.showAll = document.getElementById('show-all-toggle').checked;
  renderContainers();
}

function saveSearch() {
  localStorage.setItem('containerSearch', document.getElementById('containers-search').value);
}

document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-action]');
  if (!btn) {
    document.querySelectorAll('.action-dropdown:not(.hidden)').forEach(d => d.classList.add('hidden'));
    return;
  }

  const action = btn.dataset.action;
  if (action === 'forward') {
    const idx = parseInt(btn.dataset.idx);
    openForwardModal(idx);
  } else if (action === 'stop-forward') {
    stopForward(btn.dataset.id);
  } else if (action === 'logs') {
    openLogs(btn.dataset.id, btn.dataset.name);
  } else if (action === 'inspect') {
    openInspect(btn.dataset.id, btn.dataset.name);
  } else if (action === 'restart') {
    restartContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'stop-container') {
    stopContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'start-container') {
    startContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'pull') {
    openPull(btn.dataset.id, btn.dataset.name, btn.dataset.image);
  } else if (action === 'image-info') {
    e.preventDefault();
    showImageInfo(btn.dataset.image);
  } else if (action === 'toggle-menu') {
    e.stopPropagation();
    const menu = document.querySelector(`[data-menu-idx="${btn.dataset.idx}"]`);
    if (!menu) return;
    const wasHidden = menu.classList.contains('hidden');
    document.querySelectorAll('.action-dropdown:not(.hidden)').forEach(d => d.classList.add('hidden'));
    if (wasHidden) menu.classList.remove('hidden');
  }
});

document.getElementById('modal').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closeModal();
});

document.getElementById('inspect-modal').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closeInspect();
});

document.getElementById('pull-modal').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closePull();
});

const savedFilter = localStorage.getItem('composeFilter');
if (savedFilter) {
  try {
    const arr = JSON.parse(savedFilter);
    if (Array.isArray(arr)) state.composeFilter = arr;
  } catch (_) {}
}

const savedSearch = localStorage.getItem('containerSearch');
if (savedSearch) {
  document.getElementById('containers-search').value = savedSearch;
}

refreshContainers();
refreshForwards();

fetch('/api/version').then(r => r.json()).then(v => {
  document.getElementById('header-version').textContent = v.version;
}).catch(() => {});

const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
const fwdWs = new WebSocket(`${proto}//${location.host}/api/forwards/ws`);
fwdWs.onmessage = (e) => {
  try {
    state.forwards = JSON.parse(e.data);
    renderForwardsUI();
  } catch (_) {}
};
fwdWs.onclose = () => { setTimeout(() => { location.reload(); }, 3000); };
