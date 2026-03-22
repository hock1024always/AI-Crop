/* =========================================================
   AI Corp - Pixel Office   (Vanilla JS, zero deps)
   ========================================================= */

// 自动检测 API 地址
const API = window.location.hostname === 'localhost' 
  ? 'http://localhost:8080' 
  : `http://${window.location.hostname}:8080`;

// ---- State ----
const state = {
  agents: [],         // [{id, name, type, status, model, tools}]
  tasks: [],          // [{id, title, status, assigned_to}]
  workstations: [],   // [{id, agent?, position}]
  selectedDesk: null,
  ws: null,
  connected: false,
  selectedRole: 'developer',
  selectedModel: 'deepseek',
  // mock monitor
  cpu: 12, mem: 34, net: 5, tok: 0,
};

// ---- Init ----
document.addEventListener('DOMContentLoaded', () => {
  startClock();
  connectWebSocket();
  fetchAgents();
  fetchTasks();
  startMonitorSim();

  // Enter key in chat
  document.getElementById('chat-input').addEventListener('keydown', e => {
    if (e.key === 'Enter') sendMessage();
  });

  // Role card selection
  document.querySelectorAll('.role-card').forEach(card => {
    card.addEventListener('click', () => {
      document.querySelectorAll('.role-card').forEach(c => c.classList.remove('selected'));
      card.classList.add('selected');
      state.selectedRole = card.dataset.role;
    });
  });
});

// ---- WebSocket ----
function connectWebSocket() {
  try {
    state.ws = new WebSocket(API.replace('http', 'ws') + '/ws');

    state.ws.onopen = () => {
      state.connected = true;
      updateConnectionUI();
      appendChat('sys', '[SYSTEM] Connected to Orchestrator');
    };

    state.ws.onmessage = e => {
      try {
        const msg = JSON.parse(e.data);
        handleWSMessage(msg);
      } catch (_) {}
    };

    state.ws.onclose = () => {
      state.connected = false;
      updateConnectionUI();
      appendChat('sys', '[SYSTEM] Disconnected. Retrying in 5s...');
      setTimeout(connectWebSocket, 5000);
    };

    state.ws.onerror = () => {
      state.connected = false;
      updateConnectionUI();
    };
  } catch (_) {
    setTimeout(connectWebSocket, 5000);
  }
}

function handleWSMessage(msg) {
  if (msg.type === 'event' || msg.type === 'task_update') {
    fetchAgents();
    fetchTasks();
  }
  if (msg.type === 'agent_status') {
    const a = state.agents.find(a => a.id === msg.agentId);
    if (a) {
      a.status = msg.status;
      renderOffice();
      renderAgentDetail();
    }
  }
  if (msg.content) {
    const text = typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content);
    appendChat('agent', `[${msg.from || 'agent'}] ${text}`);
  }
}

function updateConnectionUI() {
  const dot = document.getElementById('connection-dot');
  const txt = document.getElementById('connection-text');
  dot.className = 'dot ' + (state.connected ? 'online' : 'offline');
  txt.textContent = state.connected ? 'ONLINE' : 'OFFLINE';
}

// ---- API ----
async function fetchAgents() {
  try {
    const res = await fetch(API + '/api/v1/agents');
    const data = await res.json();
    state.agents = data.agents || [];
    document.getElementById('agent-count').textContent = state.agents.length;
    syncWorkstations();
    renderOffice();
  } catch (_) {}
}

async function fetchTasks() {
  try {
    const res = await fetch(API + '/api/v1/tasks');
    const data = await res.json();
    state.tasks = data.tasks || [];
    document.getElementById('task-count').textContent = state.tasks.length;
    renderTaskBoard();
  } catch (_) {}
}

// Sync workstations with agents
function syncWorkstations() {
  // Add new agents as workstations
  state.agents.forEach(agent => {
    if (!state.workstations.find(w => w.agentId === agent.id)) {
      state.workstations.push({ id: 'ws-' + agent.id, agentId: agent.id });
    }
  });
}

// ---- Render: Office ----
function renderOffice() {
  const grid = document.getElementById('office-grid');
  grid.innerHTML = '';

  state.workstations.forEach(ws => {
    const agent = state.agents.find(a => a.id === ws.agentId);
    const div = document.createElement('div');
    div.className = 'workstation' + (agent ? '' : ' empty') +
      (agent && agent.status === 'busy' ? ' working' : '') +
      (state.selectedDesk === ws.id ? ' selected' : '');

    if (agent) {
      const statusCls = agent.status || 'idle';
      div.innerHTML = `
        <div class="desk-status-dot ${statusCls}"></div>
        <div class="desk-sprite ${agent.type}"></div>
        <div class="desk-name">${agent.name || agent.id}</div>
      `;
      div.onclick = () => selectDesk(ws.id);
    } else {
      div.innerHTML = `<div class="desk-name" style="font-size:14px; opacity:0.4">+</div>`;
      div.onclick = () => openModal();
    }

    grid.appendChild(div);
  });

  // always show one extra empty slot
  const empty = document.createElement('div');
  empty.className = 'workstation empty';
  empty.innerHTML = `<div class="desk-name" style="font-size:14px; opacity:0.4">+</div>`;
  empty.onclick = () => openModal();
  grid.appendChild(empty);
}

function selectDesk(wsId) {
  state.selectedDesk = wsId;
  renderOffice();
  renderAgentDetail();
}

function renderAgentDetail() {
  const el = document.getElementById('agent-detail-content');
  const ws = state.workstations.find(w => w.id === state.selectedDesk);
  if (!ws) {
    el.innerHTML = '<div class="empty-hint">Click a desk to see details</div>';
    return;
  }
  const agent = state.agents.find(a => a.id === ws.agentId);
  if (!agent) {
    el.innerHTML = '<div class="empty-hint">Empty desk</div>';
    return;
  }

  el.innerHTML = `
    <div class="detail-row"><span class="detail-label">ID</span><span class="detail-value">${agent.id}</span></div>
    <div class="detail-row"><span class="detail-label">NAME</span><span class="detail-value">${agent.name}</span></div>
    <div class="detail-row"><span class="detail-label">ROLE</span><span class="detail-value">${agent.type}</span></div>
    <div class="detail-row"><span class="detail-label">STATUS</span><span class="detail-value" style="color: ${statusColor(agent.status)}">${agent.status}</span></div>
    <div class="detail-row"><span class="detail-label">MODEL</span><span class="detail-value">${agent.model || 'deepseek'}</span></div>
    ${agent.current_task ? `<div class="detail-row"><span class="detail-label">TASK</span><span class="detail-value">${agent.current_task}</span></div>` : ''}
    <div style="margin-top:8px">
      <button class="pixel-btn" onclick="assignTaskToAgent('${agent.id}')" style="width:100%;margin-bottom:4px">ASSIGN TASK</button>
      <button class="pixel-btn cancel-btn" onclick="removeAgent('${agent.id}')" style="width:100%">REMOVE</button>
    </div>
  `;
}

// ---- Render: Task Board ----
function renderTaskBoard() {
  const cols = {
    pending: document.getElementById('tasks-pending'),
    running: document.getElementById('tasks-running'),
    completed: document.getElementById('tasks-completed'),
  };

  Object.values(cols).forEach(el => el.innerHTML = '');

  state.tasks.forEach(task => {
    const col = cols[task.status] || cols.pending;
    const card = document.createElement('div');
    card.className = 'task-card';
    card.innerHTML = `
      <div class="task-title">${task.title || task.id}</div>
      <div class="task-meta">${task.assigned_to ? 'Agent: ' + task.assigned_to : ''}</div>
    `;
    col.appendChild(card);
  });
}

// ---- Chat ----
function appendChat(type, text) {
  const el = document.getElementById('chat-messages');
  const div = document.createElement('div');
  div.className = type + '-msg';
  div.textContent = text;
  el.appendChild(div);
  el.scrollTop = el.scrollHeight;
}

async function sendMessage() {
  const input = document.getElementById('chat-input');
  const text = input.value.trim();
  if (!text) return;

  appendChat('user', '> ' + text);
  input.value = '';

  try {
    // Create a task from natural language
    const res = await fetch(API + '/api/v1/tasks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: text.substring(0, 80),
        description: text,
        created_by: 'user',
      }),
    });
    const data = await res.json();
    appendChat('sys', `[SYSTEM] Task created: ${data.task_id || data.id || 'OK'}`);
    fetchTasks();
    fetchAgents();
  } catch (err) {
    appendChat('error', '[ERROR] Failed to create task: ' + err.message);
  }
}

// ---- Modal ----
function openModal() {
  document.getElementById('recruit-modal').classList.remove('hidden');
  document.getElementById('employee-name').value = '';
  document.getElementById('employee-name').focus();
}

function closeModal() {
  document.getElementById('recruit-modal').classList.add('hidden');
}

// 兼容 index.html 中的函数名
function openRecruitModal() {
  openModal();
}

function closeRecruitModal() {
  closeModal();
}

function recruitEmployee() {
  confirmCreate();
}

function selectRole(el) {
  document.querySelectorAll('.role-card').forEach(c => c.classList.remove('selected'));
  el.classList.add('selected');
  state.selectedRole = el.dataset.role;
}

function selectModel(el) {
  document.querySelectorAll('.model-card').forEach(c => c.classList.remove('selected'));
  el.classList.add('selected');
  state.selectedModel = el.dataset.model;
}

async function confirmCreate() {
  const name = document.getElementById('employee-name').value.trim() ||
    state.selectedRole + '-' + Date.now().toString(36);

  try {
    await fetch(API + '/api/v1/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: name,
        type: state.selectedRole,
        model: state.selectedModel,
        resources: { cpu: '1', memory: '2Gi' },
      }),
    });
    appendChat('sys', `[SYSTEM] Agent "${name}" created (${state.selectedRole})`);
    closeModal();
    fetchAgents();
  } catch (err) {
    appendChat('error', '[ERROR] Failed to create agent: ' + err.message);
  }
}

function addWorkstation() {
  openModal();
}

// ---- Agent Actions ----
async function assignTaskToAgent(agentId) {
  const task = prompt('Enter task description:');
  if (!task) return;

  try {
    await fetch(API + '/api/v1/tasks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: task.substring(0, 80),
        description: task,
        assigned_to: agentId,
        created_by: 'user',
      }),
    });
    appendChat('sys', `[SYSTEM] Task assigned to ${agentId}`);
    fetchTasks();
    fetchAgents();
  } catch (err) {
    appendChat('error', '[ERROR] ' + err.message);
  }
}

async function removeAgent(agentId) {
  // Just remove from local workstations
  state.workstations = state.workstations.filter(w => w.agentId !== agentId);
  state.selectedDesk = null;
  renderOffice();
  renderAgentDetail();
  appendChat('sys', `[SYSTEM] Desk for ${agentId} removed`);
}

// ---- Utilities ----
function statusColor(s) {
  return { idle: '#53d769', busy: '#ffc107', offline: '#8892a0' }[s] || '#8892a0';
}

function startClock() {
  const el = document.getElementById('clock');
  setInterval(() => {
    const d = new Date();
    el.textContent = d.toLocaleTimeString('en-GB');
  }, 1000);
}

// ---- Monitor (real API + fallback simulation) ----
function startMonitorSim() {
  fetchMetrics(); // initial
  setInterval(fetchMetrics, 3000);
}

async function fetchMetrics() {
  try {
    const res = await fetch(API + '/api/v1/metrics');
    const data = await res.json();

    // System
    if (data.system) {
      state.cpu = Math.round(data.system.cpu_pct || rand(8, 25));
      state.mem = Math.round(data.system.memory_mb || 0);
      const memPct = data.system.memory_pct || Math.min(state.mem / 10, 90);
      setBar('cpu', state.cpu, state.cpu + '%');
      setBar('mem', Math.round(memPct), state.mem + 'MB');
    }

    // Network
    if (data.network) {
      const netKB = Math.round((data.network.rate_in || 0) / 1024);
      setBar('net', Math.min(netKB, 100), netKB + 'kb/s');
    } else {
      state.net = clamp(state.net + rand(-2, 2), 0, 50);
      setBar('net', state.net, state.net + 'kb/s');
    }

    // Token rate
    let tokRate = 0;
    if (data.tokens) {
      Object.values(data.tokens).forEach(t => {
        tokRate += (t.input_rate || 0) + (t.output_rate || 0);
      });
    }
    tokRate = Math.round(tokRate);
    setBar('tok', Math.min(tokRate, 100), tokRate + '/s');

  } catch (_) {
    // Fallback: simulate
    state.cpu = clamp(state.cpu + rand(-3, 3), 5, 95);
    state.mem = clamp(state.mem + rand(-2, 2), 20, 80);
    state.net = clamp(state.net + rand(-2, 2), 0, 50);
    state.tok = state.agents.some(a => a.status === 'busy') ? rand(10, 60) : 0;

    setBar('cpu', state.cpu, state.cpu + '%');
    setBar('mem', state.mem, state.mem + '%');
    setBar('net', state.net, state.net + 'kb/s');
    setBar('tok', Math.min(state.tok, 100), state.tok + '/s');
  }
}

function setBar(key, pct, label) {
  document.getElementById(key + '-bar').style.width = pct + '%';
  document.getElementById(key + '-val').textContent = label;
}

function rand(min, max) { return Math.floor(Math.random() * (max - min + 1)) + min; }
function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }
