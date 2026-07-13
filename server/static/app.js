let nextId = 1;
let allTools = [];

// --- Tabs ---
document.querySelectorAll('.tab-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    btn.classList.add('active');
    document.getElementById('tab-' + btn.dataset.tab).classList.add('active');
    if (btn.dataset.tab === 'notifications') {
      document.getElementById('notif-badge').classList.add('hidden');
      document.getElementById('notif-badge').textContent = '0';
    }
  });
});

// --- Notifications (polling) ---
let notifCount = 0;

async function pollNotifications() {
  try {
    const res = await fetch('/api/notifications');
    if (res.status !== 200) return;
    const list = await res.json();
    for (let i = 0; i < list.length; i++) {
      addNotification(list[i]);
    }
  } catch (err) {
    // ignore
  }
}

const evtStore = new EventSource('/api/events');
evtStore.onmessage = (event) => {
  const data = JSON.parse(event.data);
  if (!document.getElementById('tab-notifications').classList.contains('active')) {
    showToast(data);
  }
  addNotification(data);
};

function showToast(data) {
  const container = document.getElementById('toast-container');
  const el = document.createElement('div');
  el.className = 'toast ' + data.severity;
  el.textContent = data.message;
  container.appendChild(el);
  setTimeout(() => {
    container.removeChild(el);
  }, 8000);
}

function addNotification(data) {
  const severity = data.severity || 'info';
  const message = data.message || JSON.stringify(data);
  const table = data.table || '';
  const operation = data.operation || '';
  const ts = data.timestamp ? new Date(data.timestamp * 1000) : new Date();
  const timeStr = ts.toLocaleTimeString();

  const notifList = document.getElementById('notif-list');
  const el = document.createElement('div');
  el.className = 'notif severity-' + severity;

  const icons = { success: '\u2705', error: '\u274c', warning: '\u26a0\ufe0f', info: '\ud83d\udd14' };
  el.innerHTML =
    '<span class="icon">' + (icons[severity] || '\ud83d\udd14') + '</span>' +
    '<span class="body">' + (operation ? '<strong>' + escapeHtml(operation) + '</strong> on ' : '') +
    (table ? '<strong>' + escapeHtml(table) + '</strong>: ' : '') + escapeHtml(message) + '</span>' +
    '<span class="time">' + timeStr + '</span>';

  notifList.prepend(el);

  notifCount++;
  const badge = document.getElementById('notif-badge');
  badge.textContent = notifCount;
  badge.classList.remove('hidden');
}

// --- Tool List ---
async function loadTools() {
  const select = document.getElementById('tool-select');
  select.innerHTML = '<option value="">Loading tools...</option>';
  try {
    const mcpHeaders = {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
      'MCP-Protocol-Version': '2025-06-18'
    };

    const initRes = await fetch('/', {
      method: 'POST',
      headers: mcpHeaders,
      body: JSON.stringify({
        jsonrpc: '2.0',
        method: 'initialize',
        params: {
          protocolVersion: '2025-06-18',
          capabilities: {},
          clientInfo: { name: 'dynamodb-sage-dashboard', version: '1.0' }
        },
        id: nextId++
      })
    });
    if (!initRes.ok) {
      throw new Error('MCP initialize failed: HTTP ' + initRes.status);
    }
    await initRes.json();

    const res = await fetch('/', {
      method: 'POST',
      headers: mcpHeaders,
      body: JSON.stringify({
        jsonrpc: '2.0',
        method: 'tools/list',
        id: nextId++
      })
    });
    const body = await res.json();
    if (body.result && body.result.tools) {
      select.innerHTML = '<option value="">Select a tool...</option>';
      allTools = body.result.tools;
      allTools.sort((a, b) => a.name.localeCompare(b.name));
      allTools.forEach(t => {
        const opt = document.createElement('option');
        opt.value = t.name;
        opt.textContent = t.name;
        opt.title = t.description || '';
        select.appendChild(opt);
      });
    } else {
      select.innerHTML = '<option value="">Failed to load tools</option>';
    }
  } catch (err) {
    select.innerHTML = '<option value="">Error loading tools: ' + err.message + '</option>';
  }
}

// Populate example JSON when a tool is selected
document.getElementById('tool-select').addEventListener('change', e => {
  const toolName = e.target.value;
  const textarea = document.getElementById('args-input');
  const tool = allTools.find(t => t.name === toolName);
  if (!tool || !tool.inputSchema) {
    textarea.value = '';
    textarea.placeholder = '{}';
    return;
  }
  try {
    const exampleObj = generateExampleFromSchema(tool.inputSchema);
    textarea.value = JSON.stringify(exampleObj, null, 2);
    textarea.placeholder = '{"tableName": "..."}';
  } catch (err) {
    textarea.value = '';
    textarea.placeholder = '{}';
  }
});

function generateExampleFromSchema(schema) {
  const obj = {};
  if (!schema || !schema.properties) return obj;
  const props = schema.properties;
  for (const key in props) {
    const prop = props[key];
    // Use explicit default if present
    if (Object.prototype.hasOwnProperty.call(prop, 'default')) {
      obj[key] = prop.default;
      continue;
    }
    // Use first enum value if available
    if (prop.enum && Array.isArray(prop.enum) && prop.enum.length > 0) {
      obj[key] = prop.enum[0];
      continue;
    }
    // Extract example from description (look for "Example ..." suffix)
    if (prop.type === 'string' && prop.description) {
      const exMatch = prop.description.match(/Example\s+(.+)/);
      if (exMatch) {
        const ex = exMatch[1].replace(/\.$/, '').split(',')[0].trim();
        obj[key] = ex;
        continue;
      }
    }
    switch (prop.type) {
      case 'string':
        obj[key] = '...';
        break;
      case 'boolean':
        obj[key] = false;
        break;
      case 'integer':
      case 'number':
        obj[key] = 0;
        break;
      case 'array':
        if (prop.items && prop.items.type === 'object') {
          obj[key] = [generateExampleFromSchema(prop.items)];
        } else if (prop.items && prop.items.type === 'string') {
          obj[key] = ['...'];
        } else {
          obj[key] = [];
        }
        break;
      case 'object':
        obj[key] = generateExampleFromSchema(prop);
        break;
      default:
        obj[key] = null;
    }
  }
  return obj;
}


// --- Chat ---
document.getElementById('send-btn').addEventListener('click', sendMessage);
document.getElementById('args-input').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) sendMessage();
});

async function sendMessage() {
  const toolSelect = document.getElementById('tool-select');
  const toolName = toolSelect.value;
  const argsText = document.getElementById('args-input').value.trim();

  if (!toolName) {
    addChatMessage('user', 'Please select a tool first.');
    return;
  }

  let args = {};
  if (argsText) {
    try {
      args = JSON.parse(argsText);
    } catch (e) {
      addChatMessage('user', 'Invalid JSON arguments: ' + e.message);
      return;
    }
  }

  const displayText = toolName + (argsText ? ' ' + argsText : '');
  addChatMessage('user', displayText);

  document.getElementById('send-btn').disabled = true;
  document.getElementById('send-btn').textContent = 'Sending...';

  try {
    const res = await fetch('/', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'application/json, text/event-stream',
        'MCP-Protocol-Version': '2025-06-18'
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        method: 'tools/call',
        params: { name: toolName, arguments: args },
        id: nextId++
      })
    });
    const body = await res.json();
    if (body.result) {
      const content = body.result.content || [];
      const text = content.map(c => c.text || JSON.stringify(c)).join('\n');
      addChatMessage('server', text);
    } else if (body.error) {
      addChatMessage('server', '\u274c Error ' + body.error.code + ': ' + body.error.message);
    }
  } catch (err) {
    addChatMessage('server', '\u274c Request failed: ' + err.message);
  } finally {
    document.getElementById('send-btn').disabled = false;
    document.getElementById('send-btn').textContent = 'Send';
  }
}

function addChatMessage(role, text) {
  const container = document.getElementById('chat-messages');
  const el = document.createElement('div');
  el.className = 'msg ' + role;
  if (role === 'server') {
    el.innerHTML = '<pre>' + escapeHtml(text) + '</pre>';
  } else {
    el.textContent = text;
  }
  container.appendChild(el);
  container.scrollTop = container.scrollHeight;
}

let activeCharts = [];

// --- Metrics ---
async function fetchMetrics() {
  try {
    const res = await fetch('/api/metrics');
    const text = await res.text();
    renderMetrics(text);
  } catch (err) {
    document.getElementById('metrics-display').innerHTML = '<p>Error fetching metrics: ' + err.message + '</p>';
  }
}

function parsePrometheus(raw) {
  const byName = {};
  for (const line of raw.split('\n')) {
    if (line.startsWith('#') || line.trim() === '') continue;
    const nameMatch = line.match(/^([a-zA-Z_:][a-zA-Z0-9_:]*)/);
    if (!nameMatch) continue;
    const fullName = nameMatch[1];
    const parts = line.split(' ');
    const value = parseFloat(parts[parts.length - 1]);
    if (isNaN(value)) continue;

    let base = fullName, suffix = '';
    for (const s of ['_bucket', '_sum', '_count']) {
      if (fullName.endsWith(s)) {
        base = fullName.slice(0, -s.length);
        suffix = s;
        break;
      }
    }
    if (!byName[base]) byName[base] = { buckets: {}, sum: 0, count: 0, scalar: 0, hasScalar: false };

    if (suffix === '_bucket') {
      const le = (line.match(/le="([^"]+)"/) || [, '+Inf'])[1];
      byName[base].buckets[le] = (byName[base].buckets[le] || 0) + value;
    } else if (suffix === '_sum') {
      byName[base].sum += value;
    } else if (suffix === '_count') {
      byName[base].count += value;
    } else {
      byName[base].scalar += value;
      byName[base].hasScalar = true;
    }
  }
  return byName;
}

const metricLabels = {
  'sage_risk_analysis_total': 'Risk Analysis',
  'sage_risk_analysis_blocked_total': 'Risk Analysis Blocked',
  'sage_risk_analysis_confirmed_total': 'Risk Analysis Confirmed',
  'sage_risk_analysis_duration_seconds': 'Risk Analysis Duration',
  'sage_risk_pii_detected_total': 'PII Detected',
  'sage_audit_log_write_duration_seconds': 'Audit Log Write Duration',
  'sage_audit_buffer_depth': 'Audit Buffer Depth',
  'sage_mcp_tool_invocations_total': 'Tool Invocations',
  'sage_mcp_tool_duration_seconds': 'Tool Duration',
  'sage_mcp_tool_errors_total': 'Tool Errors',
  'sage_dynamodb_operation_total': 'DynamoDB Operations',
  'sage_dynamodb_operation_duration_seconds': 'DynamoDB Operation Duration',
  'sage_dynamodb_consumed_capacity_total': 'Consumed Capacity',
  'sage_async_jobs_total': 'Async Jobs',
  'sage_async_job_duration_seconds': 'Async Job Duration',
  'sage_queue_depth': 'Queue Depth',
  'sage_job_storage_pending': 'Pending Job Results',
  'sage_kafka_send_duration_seconds': 'Kafka Send Duration',
  'sage_kafka_send_total': 'Kafka Sends',
  'sage_kafka_send_bytes_total': 'Kafka Bytes Sent',
  'sage_kafka_consumer_lag': 'Kafka Consumer Lag',
  'go_goroutines': 'Go Routines',
};

function renderMetrics(raw) {
  for (const c of activeCharts) c.destroy();
  activeCharts = [];

  const data = parsePrometheus(raw);
  const interesting = Object.keys(metricLabels);

  const scalars = [];
  const histograms = [];

  for (const name of interesting) {
    const m = data[name];
    if (!m) continue;
    if (Object.keys(m.buckets).length > 0) {
      histograms.push({ name, data: m });
    } else {
      scalars.push({ name, value: m.hasScalar ? m.scalar : m.count });
    }
  }

  const html = [];

  if (scalars.length > 0) {
    html.push('<div class="metric-grid">');
    for (const s of scalars) {
      const v = typeof s.value === 'number' ? (Number.isInteger(s.value) ? s.value : s.value.toFixed(4)) : s.value;
      html.push('<div class="metric-card"><div class="metric-card-name">' + escapeHtml(metricLabels[s.name] || s.name) + '</div><div class="metric-card-value">' + v + '</div></div>');
    }
    html.push('</div>');
  }

  for (const h of histograms) {
    const cid = 'chart-' + h.name.replace(/[^a-zA-Z0-9]/g, '-');
    html.push('<div class="chart-container"><h4>' + escapeHtml(metricLabels[h.name] || h.name) + '</h4><canvas id="' + cid + '"></canvas></div>');
    setTimeout(() => {
      const c = buildHistogramChart(cid, h.data);
      if (c) activeCharts.push(c);
    }, 50);
  }

  if (html.length === 0) {
    html.push('<p class="text-muted">No matching metrics found yet. Run some operations first.</p>');
    html.push('<details><summary>Raw output</summary><pre>' + escapeHtml(raw.slice(0, 2000)) + '</pre></details>');
  }

  document.getElementById('metrics-display').innerHTML = html.join('');
}

function buildHistogramChart(canvasId, data) {
  const ctx = document.getElementById(canvasId);
  if (!ctx || typeof Chart === 'undefined') return null;

  const labels = Object.keys(data.buckets)
    .sort((a, b) => (a === '+Inf' ? 1 : b === '+Inf' ? -1 : parseFloat(a) - parseFloat(b)));

  const cumul = labels.map(le => data.buckets[le]);
  const perBucket = [];
  let prev = 0;
  for (const v of cumul) {
    perBucket.push(v - prev);
    prev = v;
  }

  const displayLabels = labels.map(l => (l === '+Inf' ? 'overflow' : l + 's'));

  return new Chart(ctx, {
    type: 'bar',
    data: {
      labels: displayLabels,
      datasets: [{
        label: 'Count',
        data: perBucket,
        backgroundColor: 'rgba(88,166,255,0.7)',
        borderColor: 'rgba(88,166,255,1)',
        borderWidth: 1,
        borderRadius: 3
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: ctx => 'Count: ' + ctx.parsed.y
          }
        }
      },
      scales: {
        x: {
          title: { display: true, text: 'Duration', color: '#8b949e' },
          ticks: { color: '#8b949e' }
        },
        y: {
          beginAtZero: true,
          ticks: { color: '#8b949e', precision: 0 }
        }
      }
    }
  });
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

// --- Chat ---
document.addEventListener('DOMContentLoaded', () => {
  const sendBtn = document.getElementById('llm-send-btn');
  const input = document.getElementById('llm-input');

  if (sendBtn) {
    sendBtn.addEventListener('click', sendChatMessage);
  }

  if (input) {
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendChatMessage();
      }
    });
  }
});

async function sendChatMessage() {
  const input = document.getElementById('llm-input');
  const btn = document.getElementById('llm-send-btn');
  const msg = input.value.trim();
  if (!msg) return;

  input.value = '';
  btn.disabled = true;
  btn.textContent = 'Sending...';

  try {
    addChatBubble('user', msg);
    showChatTyping();

    const res = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: msg })
    });

    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let fullText = '';
    let bubble = null;
    let lineBuffer = '';
    let eventBuffer = [];

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      lineBuffer += decoder.decode(value, { stream: true });
      const lines = lineBuffer.split('\n');
      lineBuffer = lines.pop() || '';

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          eventBuffer.push(line.slice(6));
        } else if (line.trim() === '' && eventBuffer.length > 0) {
          const eventData = eventBuffer.join('\n');
          eventBuffer = [];

          if (eventData === '[DONE]') continue;

          if (!bubble) {
            hideChatTyping();
            bubble = createStreamingBubble();
          }

          fullText += eventData;
          updateStreamingBubble(bubble, fullText);
        }
      }
    }

    if (eventBuffer.length > 0) {
      const eventData = eventBuffer.join('\n');
      if (eventData && eventData !== '[DONE]') {
        fullText += eventData;
        if (bubble) updateStreamingBubble(bubble, fullText);
      }
    }

    if (!bubble) {
      hideChatTyping();
      addChatBubble('assistant', 'No response.');
    }
  } catch (err) {
    hideChatTyping();
    addChatBubble('assistant', 'Error: ' + err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = 'Send';
  }
}

function addChatBubble(role, text) {
  const area = document.getElementById('llm-messages');
  const el = document.createElement('div');
  el.className = 'chat-msg ' + role;

  // Render markdown-like content: code blocks, bold, lists
  const html = renderChatContent(text);
  el.innerHTML = html;

  const ts = document.createElement('span');
  ts.className = 'timestamp';
  ts.textContent = new Date().toLocaleTimeString();
  el.appendChild(ts);

  area.appendChild(el);
  area.scrollTop = area.scrollHeight;
}

function createStreamingBubble() {
  const area = document.getElementById('llm-messages');
  const el = document.createElement('div');
  el.className = 'chat-msg assistant';
  el.id = 'streaming-bubble';
  el.innerHTML = '<p></p>';
  area.appendChild(el);
  area.scrollTop = area.scrollHeight;
  return el;
}

function updateStreamingBubble(el, text) {
  const html = renderChatContent(text);
  el.innerHTML = html;
  const area = document.getElementById('llm-messages');
  area.scrollTop = area.scrollHeight;
}

function renderChatContent(text) {
  // Escape HTML first
  const esc = escapeHtml(text);
  // Code blocks (```...```)
  const withCode = esc.replace(/```(\w*)\n?([\s\S]*?)```/g, '<pre><code>$2</code></pre>');
  // Inline code (`...`)
  const withInline = withCode.replace(/`([^`]+)`/g, '<code>$1</code>');
  // Bold (**...**)
  const withBold = withInline.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Bullet lists (lines starting with • or -)
  const withLists = withBold.replace(/^[•\-]\s(.+)$/gm, '<li>$1</li>');
  const hasList = withLists.includes('<li>');
  // Wrap consecutive <li> in <ul>
  const withWrapped = hasList ? withLists.replace(/(<li>.*<\/li>\n?)+/g, '<ul>$&</ul>') : withLists;
  // Paragraphs: double newlines
  const withParagraphs = withWrapped.replace(/\n\n/g, '</p><p>');
  return '<p>' + withParagraphs + '</p>';
}

function showChatTyping() {
  const area = document.getElementById('llm-messages');
  const el = document.createElement('div');
  el.className = 'chat-typing';
  el.id = 'chat-typing-indicator';
  el.innerHTML = '<span></span><span></span><span></span>';
  area.appendChild(el);
  area.scrollTop = area.scrollHeight;
}

function hideChatTyping() {
  const el = document.getElementById('chat-typing-indicator');
  if (el) el.remove();
}

// Add welcome message on load
window.addEventListener('load', () => {
  addChatBubble('assistant',
    'Hello! I can help you manage your DynamoDB tables.\n\n' +
    'Type **/help** or **/tools** to see available operations.\n' +
    'Or just ask me something like "list my tables" or "how do I query data?"'
  );
});

// --- Init ---
loadTools();
setInterval(fetchMetrics, 10000);
pollNotifications();
