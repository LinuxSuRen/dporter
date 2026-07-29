const state = {
  containers: [],
  forwards: [],
  stats: [],
  volumes: [],
  modalTarget: null,
  showAll: false,
  composeFilter: [],
  currentView: 'network',
};

function logout() {
  window.location.href = '/api/logout';
}

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

  if (state.currentView === 'network') {
    renderContainers();
    showEl('containers-table');
  } else if (state.currentView === 'performance') {
    refreshStats();
  }
}

function renderContainers() {
  if (state.currentView !== 'network') return;

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
      `<button type="button" class="btn-icon" data-action="delete-container" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}" style="color:#dc2626">Delete</button>`,
    ];
    if (c.state === 'running') {
      moreItems.push(
        `<button type="button" class="btn-icon" data-action="logs" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}">Logs</button>`,
        `<button type="button" class="btn-icon" data-action="shell" data-id="${escapeHtml(c.id)}" data-name="${escapeHtml(c.name)}">Shell</button>`,
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

let shellWs = null;
let shellTerm = null;
let shellFit = null;

function openShell(containerId, containerName) {
  closeShell();
  document.getElementById('shell-title').textContent = `Shell: ${containerName}`;
  document.getElementById('shell-terminal').innerHTML = '';
  document.getElementById('shell-drawer').classList.add('open');

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/api/containers/${encodeURIComponent(containerId)}/shell`;

  shellTerm = new Terminal({ cursorBlink: true, fontSize: 13, fontFamily: "'SF Mono',Monaco,monospace", theme: { background: '#0d1117', foreground: '#c9d1d9' } });
  if (typeof FitAddon !== 'undefined') {
    shellFit = new FitAddon.FitAddon();
    shellTerm.loadAddon(shellFit);
  }
  shellTerm.open(document.getElementById('shell-terminal'));

  shellWs = new WebSocket(url);

  shellTerm.onData(data => {
    if (shellWs && shellWs.readyState === WebSocket.OPEN) {
      shellWs.send(data);
    }
  });

  shellWs.onmessage = (e) => {
    if (e.data instanceof ArrayBuffer) {
      const str = new TextDecoder().decode(e.data);
      shellTerm.write(str);
    } else if (e.data instanceof Blob) {
      const r = new FileReader();
      r.onload = () => {
        const str = new TextDecoder().decode(r.result);
        shellTerm.write(str);
      };
      r.readAsArrayBuffer(e.data);
    } else {
      shellTerm.write(e.data);
    }
  };

  shellWs.onclose = () => {
    shellTerm.write('\r\n[disconnected]\r\n');
  };

  shellWs.onerror = () => {
    shellTerm.write('\r\n[connection error]\r\n');
  };

  if (shellFit) {
    shellFit.fit();
    window.addEventListener('resize', () => shellFit.fit());
  }
  shellTerm.focus();
}

function closeShell() {
  if (shellWs) {
    shellWs.close();
    shellWs = null;
  }
  if (shellTerm) {
    shellTerm.dispose();
    shellTerm = null;
    shellFit = null;
  }
  document.getElementById('shell-drawer').classList.remove('open');
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

async function deleteContainer(containerId, containerName) {
  if (!confirm(`Delete container "${containerName}"?\n\nThis will force-remove the container. This action cannot be undone.`)) return;
  try {
    await api(`/api/containers/${encodeURIComponent(containerId)}`, { method: 'DELETE' });
    showToast(`Deleted ${containerName}`);
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
let restartEventSource = null;

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
  if (restartEventSource) {
    restartEventSource.close();
    restartEventSource = null;
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
el.innerHTML += `<span class="compose-action" onclick="composeRestartPullAll()" title="Restart with force pull">&#8635;&#8681; Restart &amp; Pull</span>`;
el.innerHTML += `<span class="compose-action" onclick="composePullAll()" title="Pull all images">&#8681; Pull</span>`;
  }
  if (state.composeFilter.length === 1) {
el.innerHTML += `<span class="compose-action" onclick="viewComposeFile()" title="View compose file content">&#128196; View</span>`;
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
  if (state.currentView === 'performance') {
    renderStats();
  } else {
    renderContainers();
  }
}

function composeRestartAll() {
  const project = state.composeFilter[0];
  if (!project) return;
  if (!confirm(`Restart all containers in "${project}"?`)) return;

  closePull();
  document.getElementById('pull-title').textContent = `Restart: ${project}`;
  document.getElementById('pull-status').textContent = 'Starting...';
  document.getElementById('pull-layers').innerHTML = '';
  document.getElementById('pull-modal').classList.remove('hidden');

  restartEventSource = new EventSource(`/api/compose/restart?project=${encodeURIComponent(project)}`);

  const steps = [];

  restartEventSource.addEventListener('restart-error', (e) => {
    document.getElementById('pull-status').textContent = `Error: ${e.data || 'failed'}`;
    restartEventSource.close();
    restartEventSource = null;
    setTimeout(refreshContainers, 2000);
  });

  restartEventSource.addEventListener('done', () => {
    document.getElementById('pull-status').textContent = `Restart complete for ${project}`;
    showToast(`Restarted all containers in ${project}`);
    restartEventSource.close();
    restartEventSource = null;
    setTimeout(refreshContainers, 2000);
  });

  restartEventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (msg.type === 'restart') {
        if (msg.phase === 'containers') {
          const names = msg.names || [];
          document.getElementById('pull-title').textContent = `Restart: ${project} (${names.length} services)`;
          document.getElementById('pull-layers').innerHTML = names.map(n =>
            `<div class="pull-layer" style="font-family:monospace;font-size:0.75rem">${escapeHtml(n)}</div>`
          ).join('');
        } else if (msg.phase === 'down' || msg.phase === 'up') {
          const label = msg.phase === 'down' ? 'Stopping' : 'Starting';
          document.getElementById('pull-status').textContent = `${label} services...`;
        } else if (msg.phase === 'output' && msg.text) {
          steps.push(msg.text);
          if (steps.length > 50) steps.shift();
          document.getElementById('pull-layers').innerHTML = steps.map(t =>
            `<div class="pull-layer" style="font-family:monospace;font-size:0.7rem;padding:2px 0">${escapeHtml(t)}</div>`
          ).join('');
        }
      }
    } catch (_) {}
  };

  restartEventSource.onerror = () => {
    if (restartEventSource && restartEventSource.readyState === EventSource.CLOSED) {
      document.getElementById('pull-status').textContent = 'Disconnected';
    }
  };
}

function composeRestartPullAll() {
  const project = state.composeFilter[0];
  if (!project) return;
  if (!confirm(`Restart & pull all images for "${project}"?`)) return;

  closePull();
  document.getElementById('pull-title').textContent = `Restart & Pull: ${project}`;
  document.getElementById('pull-status').textContent = 'Starting...';
  document.getElementById('pull-layers').innerHTML = '';
  document.getElementById('pull-modal').classList.remove('hidden');

  restartEventSource = new EventSource(`/api/compose/restart-pull?project=${encodeURIComponent(project)}`);

  const steps = [];
  const pullContainers = {};
  const pullLayers = {};
  let pullCurrentImage = '';

  restartEventSource.addEventListener('restart-error', (e) => {
    document.getElementById('pull-status').textContent = `Error: ${e.data || 'failed'}`;
    restartEventSource.close();
    restartEventSource = null;
    setTimeout(refreshContainers, 2000);
  });

  restartEventSource.addEventListener('done', () => {
    document.getElementById('pull-status').textContent = `Done for ${project}`;
    showToast(`Restart & pull complete`);
    restartEventSource.close();
    restartEventSource = null;
    setTimeout(refreshContainers, 2000);
  });

  restartEventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (msg.type === 'restart') {
        if (msg.phase === 'containers') {
          const names = msg.names || [];
          document.getElementById('pull-title').textContent = `Restart & Pull: ${project} (${names.length} services)`;
          document.getElementById('pull-layers').innerHTML = names.map(n =>
            `<div class="pull-layer" style="font-family:monospace;font-size:0.75rem">${escapeHtml(n)}</div>`
          ).join('');
        } else if (msg.phase === 'down') {
          document.getElementById('pull-status').textContent = msg.status === 'running' ? 'Stopping services...' : 'Down complete';
        } else if (msg.phase === 'pull') {
          document.getElementById('pull-status').textContent = msg.status === 'running' ? 'Pulling images...' : 'Pull complete';
          if (msg.status === 'done') {
            renderComposePullList(pullContainers, pullLayers, pullCurrentImage);
          }
        } else if (msg.phase === 'up') {
          document.getElementById('pull-status').textContent = msg.status === 'running' ? 'Starting services...' : 'Up complete';
        } else if (msg.phase === 'output' && msg.text) {
          steps.push(msg.text);
          if (steps.length > 50) steps.shift();
          document.getElementById('pull-layers').innerHTML = steps.map(t =>
            `<div class="pull-layer" style="font-family:monospace;font-size:0.7rem;padding:2px 0">${escapeHtml(t)}</div>`
          ).join('');
        }
      } else if (msg.type === 'container') {
        pullContainers[msg.idx] = msg;
        if (msg.status === 'pulling') {
          if (msg.image && msg.image !== pullCurrentImage) {
            for (const k in pullLayers) delete pullLayers[k];
            pullCurrentImage = msg.image;
          }
          document.getElementById('pull-status').textContent = `Pulling ${msg.name}: ${msg.image}`;
        }
        renderComposePullList(pullContainers, pullLayers, pullCurrentImage);
      } else if (msg.id && msg.status) {
        const isTag = /pulling from/i.test(msg.status || '');
        if (!isTag) {
          const layer = pullLayers[msg.id] || (pullLayers[msg.id] = {});
          if (msg.progressDetail) {
            layer.current = msg.progressDetail.current || 0;
            layer.total = msg.progressDetail.total || layer.total || 1;
          }
          if (/complete|exists/i.test(msg.status || '')) {
            layer.current = layer.total || 1;
          }
          renderPullPhase(pullContainers, pullLayers, pullCurrentImage);
        }
        document.getElementById('pull-status').textContent = msg.status;
      }
    } catch (_) {}
  };

  restartEventSource.onerror = () => {
    if (restartEventSource && restartEventSource.readyState === EventSource.CLOSED) {
      document.getElementById('pull-status').textContent = 'Disconnected';
    }
  };
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
  const layers = {};
  let currentImage = '';

  pullEventSource.addEventListener('pull-error', (e) => {
    document.getElementById('pull-status').textContent = `Error: ${e.data || 'failed'}`;
    pullEventSource.close();
  });

  pullEventSource.addEventListener('done', () => {
    Object.values(containers).forEach(c => { c.status = c.status || 'done'; });
    renderComposePullList(containers, layers, currentImage);
    document.getElementById('pull-status').textContent = `Pull complete for ${project}`;
    showToast(`Pulled all images for ${project}`);
    pullEventSource.close();
  });

  pullEventSource.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      if (msg.type === 'container') {
        containers[msg.idx] = msg;
        if (msg.status === 'pulling') {
          if (msg.image && msg.image !== currentImage) {
            for (const k in layers) delete layers[k];
            currentImage = msg.image;
          }
          document.getElementById('pull-status').textContent = `Pulling ${msg.name}: ${msg.image}`;
        }
        renderComposePullList(containers, layers, currentImage);
      } else if (msg.id && msg.status) {
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
          renderComposePullList(containers, layers, currentImage);
        }
        document.getElementById('pull-status').textContent = msg.status;
      }
    } catch (_) {}
  };

  pullEventSource.onerror = () => {
    if (pullEventSource.readyState === EventSource.CLOSED) {
      document.getElementById('pull-status').textContent = 'Disconnected';
    }
  };
}

function renderComposePullList(containers, layers, currentImage) {
  const el = document.getElementById('pull-layers');

  let html = Object.values(containers).map(c => {
    let icon, cls;
    switch (c.status) {
      case 'pulling': icon = '\u2B07'; cls = 'color:#4a6cf7'; break;
      case 'done': icon = '\u2713'; cls = 'color:#059669'; break;
      case 'error': icon = '\u2717'; cls = 'color:#dc2626'; break;
      default: icon = '\u23F3'; cls = 'color:#94a3b8'; break;
    }
    return `<div class="pull-layer">
      <span style="${cls};width:16px;flex-shrink:0">${icon}</span>
      <span style="flex:1;font-family:monospace;font-size:0.75rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escapeHtml(c.name)}</span>
      <span style="color:#94a3b8;font-size:0.7rem;flex-shrink:0">${escapeHtml(c.image ? c.image.substring(0, 40) : '')}</span>
    </div>`;
  }).join('');

  if (layers && Object.keys(layers).length > 0 && currentImage) {
    html += `<div style="margin-top:8px;border-top:1px solid #e2e8f0;padding-top:8px">
      <div style="font-size:0.7rem;color:#64748b;margin-bottom:4px">${escapeHtml(currentImage)}</div>`;
    html += Object.entries(layers).map(([id, l]) => {
      const pct = l.total ? Math.round((l.current / l.total) * 100) : 100;
      const done = l.current >= l.total;
      return `<div class="pull-layer" style="font-size:0.7rem">
        <span style="width:16px;flex-shrink:0;color:${done ? '#059669' : '#4a6cf7'}">${done ? '\u2713' : '\u2B07'}</span>
        <span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:monospace;font-size:0.65rem">${escapeHtml(id.substring(0, 12))}</span>
        <span style="width:60px;text-align:right;font-family:monospace;font-size:0.65rem;flex-shrink:0">${done ? 'Done' : pct + '%'}</span>
      </div>`;
    }).join('');
    html += '</div>';
  }

  el.innerHTML = html;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function toggleShowAll() {
  state.showAll = document.getElementById('show-all-toggle').checked;
  if (state.currentView === 'performance') {
    renderStats();
  } else if (state.currentView === 'volumes') {
    renderVolumes();
  } else {
    renderContainers();
  }
}

function switchView(view) {
  state.currentView = view;
  document.getElementById('view-network').classList.toggle('active', view === 'network');
  document.getElementById('view-performance').classList.toggle('active', view === 'performance');
  document.getElementById('view-volumes').classList.toggle('active', view === 'volumes');

  document.getElementById('network-table').classList.add('hidden');
  document.getElementById('performance-table').classList.add('hidden');
  document.getElementById('volumes-table').classList.add('hidden');

  if (view === 'network') {
    document.getElementById('network-table').classList.remove('hidden');
    renderContainers();
  } else if (view === 'performance') {
    document.getElementById('performance-table').classList.remove('hidden');
    renderComposeTags(state.stats.length);
    refreshStats();
  } else if (view === 'volumes') {
    document.getElementById('volumes-table').classList.remove('hidden');
    document.getElementById('compose-tags').classList.add('hidden');
    refreshVolumes();
  }
}

let statsEventSource = null;

async function refreshStats() {
  if (statsEventSource) {
    statsEventSource.close();
    statsEventSource = null;
  }

  hideEl('containers-empty');
  hideEl('containers-error');
  showEl('containers-loading');

  state.stats = [];
  document.getElementById('stats-tbody').innerHTML = '';

  statsEventSource = new EventSource('/api/containers/stats/stream');

  statsEventSource.onmessage = (e) => {
    try {
      const s = JSON.parse(e.data);
      state.stats.push(s);
      hideEl('containers-loading');
      showEl('containers-table');
      renderStats();
    } catch (_) {}
  };

  statsEventSource.addEventListener('done', () => {
    hideEl('containers-loading');
    if (!state.stats.length) {
      showEl('containers-empty');
    } else {
      showEl('containers-table');
    }
    statsEventSource.close();
    statsEventSource = null;
  });

  statsEventSource.addEventListener('error', (e) => {
    hideEl('containers-loading');
    showEl('containers-error');
    document.getElementById('containers-error-msg').textContent =
      'Failed to load stats: ' + (e.data || 'connection error');
    statsEventSource.close();
    statsEventSource = null;
  });

  statsEventSource.onerror = () => {
    if (statsEventSource && statsEventSource.readyState === EventSource.CLOSED) {
      hideEl('containers-loading');
      if (!state.stats.length) {
        showEl('containers-error');
        document.getElementById('containers-error-msg').textContent =
          'Failed to load stats: connection closed';
      }
    }
  };
}

function renderStats() {
  const search = (document.getElementById('containers-search').value || '').toLowerCase();
  const filtered = state.stats.filter(s => {
    if (!state.showAll && s.state !== 'running') return false;
    if (state.composeFilter.length && !state.composeFilter.includes(s.composeProject || '')) return false;
    if (!search) return true;
    return s.name.toLowerCase().includes(search)
      || s.id.toLowerCase().includes(search);
  });

  renderComposeTags(filtered.length);

  const tbody = document.getElementById('stats-tbody');
  if (filtered.length === 0 && search) {
    tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:#94a3b8;padding:32px">No containers matching &ldquo;${escapeHtml(search)}&rdquo;</td></tr>`;
    return;
  }
  tbody.innerHTML = filtered.map((s) => {
    const stateClass = s.state === 'running' ? 'running' : 'stopped';

    const cpuCls = s.cpuPercent > 80 ? 'high' : s.cpuPercent > 50 ? 'mid' : 'low';
    const cpuBar = `<div class="stats-bar"><div class="stats-bar-fill ${cpuCls}" style="width:${Math.min(s.cpuPercent, 100)}%"></div></div>`;
    const cpuText = `${s.cpuPercent.toFixed(1)}%`;

    const memCls = s.memoryPercent > 90 ? 'high' : s.memoryPercent > 60 ? 'mid' : 'low';
    const memBar = `<div class="stats-bar"><div class="stats-bar-fill ${memCls}" style="width:${Math.min(s.memoryPercent, 100)}%"></div></div>`;
    const memTotal = s.memoryLimit > 0 ? ` / ${formatBytes(s.memoryLimit)}` : '';
    const memText = `${formatBytes(s.memoryUsage)}${memTotal}`;
    const memPct = `${s.memoryPercent.toFixed(1)}%`;

    return `<tr>
      <td><strong>${escapeHtml(s.name)}</strong><br><span style="font-size:0.7rem;color:#94a3b8">${escapeHtml(s.id)}</span></td>
      <td><span class="state state-${stateClass}">${escapeHtml(s.state)}</span></td>
      <td><div class="stats-cell">${cpuText}</div>${cpuBar}</td>
      <td><div class="stats-cell">${memText}</div>${memBar}</td>
      <td class="stats-cell">${memPct}</td>
    </tr>`;
  }).join('');
}

async function showVolumeDetail(name) {
  document.getElementById('volume-detail-title').textContent = `Volume: ${name}`;
  document.getElementById('volume-detail-loading').classList.remove('hidden');
  document.getElementById('volume-detail-content').classList.add('hidden');
  document.getElementById('volume-detail-modal').classList.remove('hidden');

  try {
    const data = await api(`/api/volumes/${encodeURIComponent(name)}`);
    renderVolumeDetail(data);
  } catch (err) {
    document.getElementById('volume-detail-loading').classList.add('hidden');
    document.getElementById('volume-detail-content').classList.remove('hidden');
    document.getElementById('volume-detail-content').innerHTML =
      `<div class="inspect-content" style="color:#f85149">Error: ${escapeHtml(err.message)}</div>`;
  }
}

function renderVolumeDetail(d) {
  document.getElementById('volume-detail-loading').classList.add('hidden');
  document.getElementById('volume-detail-content').classList.remove('hidden');

  const labelRows = d.labels ? Object.entries(d.labels).map(([k, v]) =>
    `<tr><td style="padding:4px 12px;color:#64748b;font-size:0.75rem">${escapeHtml(k)}</td><td style="padding:4px 12px;font-family:monospace;font-size:0.75rem">${escapeHtml(v)}</td></tr>`
  ).join('') : '';

  const optionRows = d.options ? Object.entries(d.options).map(([k, v]) =>
    `<tr><td style="padding:4px 12px;color:#64748b;font-size:0.75rem">${escapeHtml(k)}</td><td style="padding:4px 12px;font-family:monospace;font-size:0.75rem">${escapeHtml(v)}</td></tr>`
  ).join('') : '';

  const containerRows = d.containers && d.containers.length
    ? d.containers.map(c =>
        `<tr>
          <td style="padding:4px 12px"><strong>${escapeHtml(c.containerName)}</strong></td>
          <td style="padding:4px 12px;font-family:monospace;font-size:0.75rem;color:#94a3b8">${escapeHtml(c.containerId)}</td>
          <td style="padding:4px 12px;font-family:monospace;font-size:0.75rem">${escapeHtml(c.destination)}</td>
          <td style="padding:4px 12px;color:#64748b">${escapeHtml(c.mode || 'rw')}</td>
        </tr>`
      ).join('')
    : `<tr><td colspan="4" style="padding:8px 12px;color:#94a3b8;text-align:center">No containers using this volume</td></tr>`;

  document.getElementById('volume-detail-content').innerHTML = `
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:16px">
      <div class="form-group"><label>Name</label><input readonly value="${escapeHtml(d.name)}" style="background:#f8fafc;font-family:monospace;font-size:0.75rem"></div>
      <div class="form-group"><label>Driver</label><input readonly value="${escapeHtml(d.driver)}" style="background:#f8fafc"></div>
      <div class="form-group"><label>Scope</label><input readonly value="${escapeHtml(d.scope)}" style="background:#f8fafc"></div>
      <div class="form-group"><label>Created</label><input readonly value="${escapeHtml(d.createdAt)}" style="background:#f8fafc;font-size:0.75rem"></div>
    </div>
    <div class="form-group"><label>Mountpoint</label><input readonly value="${escapeHtml(d.mountpoint)}" style="background:#f8fafc;font-family:monospace;font-size:0.75rem;width:100%"></div>
    ${d.labels && Object.keys(d.labels).length ? `
      <div style="margin-top:16px"><label style="font-weight:600;font-size:0.85rem;margin-bottom:8px;display:block">Labels</label>
      <table style="width:100%;border:1px solid #e2e8f0;border-radius:6px;overflow:hidden"><tbody>${labelRows}</tbody></table></div>
    ` : ''}
    ${d.options && Object.keys(d.options).length ? `
      <div style="margin-top:16px"><label style="font-weight:600;font-size:0.85rem;margin-bottom:8px;display:block">Options</label>
      <table style="width:100%;border:1px solid #e2e8f0;border-radius:6px;overflow:hidden"><tbody>${optionRows}</tbody></table></div>
    ` : ''}
    <div style="margin-top:16px">
      <label style="font-weight:600;font-size:0.85rem;margin-bottom:8px;display:block">Containers (${d.containers ? d.containers.length : 0})</label>
      <table style="width:100%;border:1px solid #e2e8f0;border-radius:6px;overflow:hidden">
        <thead><tr><th style="width:30%">Container</th><th style="width:15%">ID</th><th style="width:35%">Mount Path</th><th style="width:20%">Mode</th></tr></thead>
        <tbody>${containerRows}</tbody>
      </table>
    </div>
  `;
}

function closeVolumeDetail() {
  document.getElementById('volume-detail-modal').classList.add('hidden');
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function saveSearch() {
  localStorage.setItem('containerSearch', document.getElementById('containers-search').value);
}

function handleSearch() {
  if (state.currentView === 'performance') {
    renderStats();
  } else if (state.currentView === 'volumes') {
    renderVolumes();
  } else {
    renderContainers();
  }
}

async function refreshVolumes() {
  hideEl('containers-empty');
  hideEl('containers-error');
  hideEl('containers-table');
  showEl('containers-loading');

  try {
    state.volumes = await api('/api/volumes');
  } catch (err) {
    hideEl('containers-loading');
    showEl('containers-error');
    document.getElementById('containers-error-msg').textContent =
      `Failed to load volumes: ${err.message}`;
    return;
  }

  hideEl('containers-loading');
  if (!state.volumes.length) {
    showEl('containers-empty');
    return;
  }
  showEl('containers-table');
  renderVolumes();
}

function renderVolumes() {
  const search = (document.getElementById('containers-search').value || '').toLowerCase();
  const filtered = state.volumes.filter(v => {
    if (!search) return true;
    return v.name.toLowerCase().includes(search)
      || v.driver.toLowerCase().includes(search)
      || v.mountpoint.toLowerCase().includes(search);
  });

  const composeDisplay = state.composeFilter.length === 0 || state.composeFilter.some(p =>
    filtered.some(v => v.composeProject === p)
  );

  const tbody = document.getElementById('volumes-tbody');
  if (filtered.length === 0 && search) {
    tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:#94a3b8;padding:32px">No volumes matching &ldquo;${escapeHtml(search)}&rdquo;</td></tr>`;
    return;
  }
  tbody.innerHTML = filtered.map((v) => {
    const sizeText = v.sizeBytes >= 0 ? formatBytes(v.sizeBytes) : '<span style="color:#94a3b8">&mdash;</span>';
    const refText = v.refCount >= 0 ? v.refCount : '<span style="color:#94a3b8">&mdash;</span>';
    const composeBadge = v.composeProject
      ? `<span class="compose-badge" title="compose project">${escapeHtml(v.composeProject)}</span>`
      : '';
    const displayName = v.name.length > 30 ? v.name.substring(0, 12) : v.name;
    const nameTitle = v.name.length > 30 ? ` title="${escapeHtml(v.name)}"` : '';

    return `<tr onclick="showVolumeDetail('${escapeHtml(v.name)}')" style="cursor:pointer">
      <td><strong${nameTitle}>${escapeHtml(displayName)}</strong>${composeBadge}</td>
      <td>${escapeHtml(v.driver)}</td>
      <td><span style="font-family:monospace;font-size:0.75rem;word-break:break-all">${escapeHtml(v.mountpoint)}</span></td>
      <td>${escapeHtml(v.scope)}</td>
      <td>${sizeText}</td>
      <td>${refText}</td>
    </tr>`;
  }).join('');
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
  } else if (action === 'shell') {
    openShell(btn.dataset.id, btn.dataset.name);
  } else if (action === 'inspect') {
    openInspect(btn.dataset.id, btn.dataset.name);
  } else if (action === 'restart') {
    restartContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'stop-container') {
    stopContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'start-container') {
    startContainer(btn.dataset.id, btn.dataset.name);
  } else if (action === 'delete-container') {
    deleteContainer(btn.dataset.id, btn.dataset.name);
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

async function viewComposeFile() {
  const project = state.composeFilter[0];
  if (!project) return;

  document.getElementById('compose-file-title').textContent = `Compose: ${project}`;
  document.getElementById('compose-file-loading').classList.remove('hidden');
  document.getElementById('compose-file-result').classList.add('hidden');
  document.getElementById('compose-file-modal').classList.remove('hidden');

  try {
    const data = await api(`/api/compose/${encodeURIComponent(project)}/file`);
    showComposeFileContent(data);
  } catch (err) {
    showComposeFileContent({ error: err.message });
  }
}

function showComposeFileContent(data) {
  document.getElementById('compose-file-loading').classList.add('hidden');
  const result = document.getElementById('compose-file-result');
  result.classList.remove('hidden');

  if (data.error) {
    result.innerHTML = `<div class="inspect-content" style="color:#f85149">Error: ${escapeHtml(data.error)}</div>`;
    return;
  }

  result.innerHTML = (data.files || []).map(f => `
    <div class="compose-file-path">${escapeHtml(f.path)}</div>
    <div class="compose-file-content">${escapeHtml(f.content)}</div>
  `).join('');
}

function closeComposeFile() {
  document.getElementById('compose-file-modal').classList.add('hidden');
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
