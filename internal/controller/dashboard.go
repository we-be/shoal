package controller

import (
	"encoding/json"
	"net/http"
	"time"
)

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Shoal Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0a0e17; color: #c9d1d9; font-family: 'SF Mono', 'Cascadia Code', 'Fira Code', monospace; font-size: 14px; padding: 20px; }
  h1 { color: #58a6ff; font-size: 18px; margin-bottom: 4px; }
  .subtitle { color: #484f58; font-size: 12px; margin-bottom: 20px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; margin-bottom: 20px; }
  .card { background: #161b22; border: 1px solid #21262d; border-radius: 8px; padding: 16px; }
  .card h2 { color: #8b949e; font-size: 11px; text-transform: uppercase; letter-spacing: 1px; margin-bottom: 12px; }
  .stat { font-size: 32px; font-weight: bold; color: #58a6ff; }
  .stat-label { color: #484f58; font-size: 11px; margin-top: 2px; }
  .stat-row { display: flex; gap: 24px; margin-bottom: 8px; }
  .stat-sm { font-size: 20px; font-weight: bold; }
  .green { color: #3fb950; }
  .yellow { color: #d29922; }
  .red { color: #f85149; }
  .cyan { color: #39d2c0; }
  .purple { color: #bc8cff; }
  table { width: 100%; border-collapse: collapse; }
  th { color: #484f58; font-size: 11px; text-transform: uppercase; letter-spacing: 1px; text-align: left; padding: 6px 8px; border-bottom: 1px solid #21262d; }
  td { padding: 6px 8px; border-bottom: 1px solid #161b22; white-space: nowrap; }
  .fish-id { color: #58a6ff; font-weight: bold; }
  .tag { display: inline-block; padding: 2px 6px; border-radius: 4px; font-size: 11px; font-weight: bold; }
  .tag-heavy { background: #f8514922; color: #f85149; }
  .tag-light { background: #3fb95022; color: #3fb950; }
  .tag-available { background: #3fb95022; color: #3fb950; }
  .tag-leased { background: #d2992222; color: #d29922; }
  .tag-cf { background: #58a6ff22; color: #58a6ff; }
  .domain-list { font-size: 12px; color: #8b949e; max-height: 60px; overflow-y: auto; white-space: normal; }
  .domain-list span { margin-right: 6px; display: inline-block; margin-bottom: 2px; }
  .bar-container { height: 6px; background: #21262d; border-radius: 3px; overflow: hidden; margin-top: 4px; }
  .bar { height: 100%; border-radius: 3px; transition: width 0.5s ease; }
  .bar-green { background: #3fb950; }
  .bar-yellow { background: #d29922; }
  .bar-red { background: #f85149; }
  .pulse { animation: pulse 2s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
  .charts { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 16px; }
  canvas { width: 100% !important; height: 120px !important; }
  .footer { color: #484f58; font-size: 11px; margin-top: 20px; text-align: center; }
  .refresh-indicator { position: fixed; top: 12px; right: 20px; font-size: 11px; color: #484f58; }
</style>
</head>
<body>

<h1>shoal</h1>
<p class="subtitle">browser orchestration dashboard</p>
<div class="refresh-indicator pulse" id="tick"></div>

<div class="grid" id="stats"></div>

<div class="charts">
  <div class="card">
    <h2>throughput (req/5s)</h2>
    <canvas id="chart-throughput"></canvas>
  </div>
  <div class="card">
    <h2>errors & cf solves</h2>
    <canvas id="chart-errors"></canvas>
  </div>
</div>

<div class="card" style="margin-bottom: 16px">
  <h2>the school</h2>
  <table>
    <thead>
      <tr>
        <th>fish</th>
        <th>backend</th>
        <th>class</th>
        <th>state</th>
        <th>ip</th>
        <th>uses</th>
        <th>domains</th>
      </tr>
    </thead>
    <tbody id="agents"></tbody>
  </table>
</div>

<div class="card">
  <h2>metrics</h2>
  <div id="metrics" style="font-size: 12px; color: #8b949e;"></div>
</div>

<p class="footer">auto-refreshes every 2s</p>

<script>
async function fetchJSON(path) {
  const r = await fetch(path);
  return r.json();
}

async function fetchText(path) {
  const r = await fetch(path);
  return r.text();
}

function renderStats(pool, agents) {
  const heavy = agents.filter(a => a.class === 'heavy');
  const medium = agents.filter(a => a.class === 'medium');
  const light = agents.filter(a => a.class === 'light');
  const cfAgents = agents.filter(a => {
    const domains = a.domains || {};
    return Object.values(domains).some(d => d.has_cf_clearance);
  });

  const pct = pool.total > 0 ? Math.round((pool.available / pool.total) * 100) : 0;
  const barClass = pct > 60 ? 'bar-green' : pct > 20 ? 'bar-yellow' : 'bar-red';

  document.getElementById('stats').innerHTML = [
    card('Pool', [
      stat(pool.total, 'total agents', ''),
      '<div class="bar-container"><div class="bar ' + barClass + '" style="width:' + pct + '%"></div></div>',
      '<div style="margin-top:8px" class="stat-row">' +
        miniStat(pool.available, 'available', 'green') +
        miniStat(pool.leased, 'leased', 'yellow') +
      '</div>',
    ]),
    card('Fleet', [
      '<div class="stat-row">' +
        miniStat(heavy.length, 'groupers', 'red') +
        miniStat(medium.length, 'redfish', 'cyan') +
        miniStat(light.length, 'minnows', 'green') +
      '</div>',
      '<div class="stat-row">' +
        miniStat(cfAgents.length, 'cf clearance', 'cyan') +
      '</div>',
    ]),
    card('Activity', [
      '<div id="activity-stats"></div>',
    ]),
    card('Tides', [
      '<div id="tides-stats"></div>',
    ]),
  ].join('');
}

function renderAgents(agents, pool) {
  // Sort by ID for stable ordering
  const sorted = [...agents].sort((a, b) => a.id.localeCompare(b.id));
  const tbody = document.getElementById('agents');
  tbody.innerHTML = sorted.map(a => {
    const domains = a.domains || {};
    const domainParts = Object.entries(domains).map(([d, s]) => {
      let info = s.visit_count + 'v';
      const cookies = (s.cookies || []).length;
      if (cookies) info += ',' + cookies + 'c';
      if (s.has_cf_clearance) info = '<span class="tag tag-cf">CF</span> ' + info;
      return '<span>' + d + '(' + info + ')</span>';
    });

    return '<tr>' +
      '<td class="fish-id">' + a.id + '</td>' +
      '<td>' + a.backend + '</td>' +
      '<td><span class="tag tag-' + a.class + '">' + a.class + '</span></td>' +
      '<td></td>' +
      '<td style="color:#484f58">' + (a.ip || '?') + '</td>' +
      '<td>' + a.use_count + '</td>' +
      '<td class="domain-list">' + (domainParts.join('') || '<span style="color:#21262d">none</span>') + '</td>' +
    '</tr>';
  }).join('');
}

function renderMetrics(text) {
  const lines = text.split('\n').filter(l => l.startsWith('shoal_'));
  const grouped = {};
  lines.forEach(l => {
    const [key, val] = l.split(' ');
    const base = key.replace(/\{.*\}/, '').replace('shoal_', '');
    if (base.includes('bucket') || base.includes('_created')) return;
    if (!grouped[base]) grouped[base] = [];
    const labels = (key.match(/\{(.*?)\}/) || ['', ''])[1];
    grouped[base].push({ labels, val: parseFloat(val) });
  });

  // Update activity card with key counters
  const get = (name) => {
    const items = grouped[name] || [];
    return items.reduce((s, i) => s + i.val, 0);
  };

  const actEl = document.getElementById('activity-stats');
  if (actEl) {
    const blocked = get('remora_blocked_total');
    const goodQ = (grouped['remora_quality_total'] || []).find(i => i.labels.includes('good'));
    const goodCount = goodQ ? goodQ.val : 0;
    actEl.innerHTML =
      '<div class="stat-row">' +
        miniStat(get('request_total'), 'requests', '') +
        miniStat(get('cf_solves_total'), 'cf solves', 'cyan') +
      '</div>' +
      '<div class="stat-row">' +
        miniStat(Math.round(goodCount), 'good', 'green') +
        miniStat(Math.round(blocked), 'blocked', 'red') +
        miniStat(get('lease_queued_total'), 'queued', 'yellow') +
      '</div>';
  }

  // Render metrics table
  const el = document.getElementById('metrics');
  const rows = Object.entries(grouped).map(([base, items]) => {
    const parts = items.map(i => {
      const lab = i.labels ? '<span style="color:#484f58">{' + i.labels + '}</span> ' : '';
      return lab + '<span style="color:#c9d1d9">' + (Number.isInteger(i.val) ? i.val : i.val.toFixed(3)) + '</span>';
    }).join(' &nbsp; ');
    return '<div style="margin-bottom:4px"><span style="color:#58a6ff">' + base + '</span> ' + parts + '</div>';
  }).join('');
  el.innerHTML = rows || '<span style="color:#21262d">no metrics yet</span>';
}

function card(title, content) {
  return '<div class="card"><h2>' + title + '</h2>' + content.join('') + '</div>';
}
function stat(val, label, color) {
  return '<div class="stat ' + color + '">' + val + '</div><div class="stat-label">' + label + '</div>';
}
function miniStat(val, label, color) {
  return '<div><div class="stat-sm ' + color + '">' + val + '</div><div class="stat-label">' + label + '</div></div>';
}

// --- Canvas timeseries charts ---

function drawChart(canvasId, buckets, series) {
  const canvas = document.getElementById(canvasId);
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  ctx.scale(dpr, dpr);
  const W = rect.width, H = rect.height;

  ctx.clearRect(0, 0, W, H);

  if (buckets.length < 2) {
    ctx.fillStyle = '#21262d';
    ctx.font = '11px monospace';
    ctx.fillText('waiting for data...', W/2 - 50, H/2);
    return;
  }

  // Find max value across all series
  let maxVal = 1;
  for (const s of series) {
    for (const b of buckets) {
      if (b[s.key] > maxVal) maxVal = b[s.key];
    }
  }
  maxVal = Math.ceil(maxVal * 1.2);

  const pad = { l: 30, r: 10, t: 5, b: 20 };
  const cw = W - pad.l - pad.r;
  const ch = H - pad.t - pad.b;

  // Grid lines
  ctx.strokeStyle = '#21262d';
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = pad.t + (ch / 4) * i;
    ctx.beginPath();
    ctx.moveTo(pad.l, y);
    ctx.lineTo(W - pad.r, y);
    ctx.stroke();
  }

  // Y-axis labels
  ctx.fillStyle = '#484f58';
  ctx.font = '10px monospace';
  ctx.textAlign = 'right';
  for (let i = 0; i <= 4; i++) {
    const val = Math.round(maxVal * (4 - i) / 4);
    const y = pad.t + (ch / 4) * i + 3;
    ctx.fillText(val, pad.l - 4, y);
  }

  // X-axis time labels
  ctx.textAlign = 'center';
  const now = Math.floor(Date.now() / 1000);
  for (let i = 0; i < buckets.length; i += Math.max(1, Math.floor(buckets.length / 6))) {
    const x = pad.l + (i / (buckets.length - 1)) * cw;
    const ago = now - buckets[i].t;
    const label = ago < 60 ? ago + 's' : Math.floor(ago/60) + 'm';
    ctx.fillText('-' + label, x, H - 4);
  }

  // Draw each series
  for (const s of series) {
    ctx.strokeStyle = s.color;
    ctx.lineWidth = 2;
    ctx.beginPath();

    // Fill area
    ctx.fillStyle = s.color + '18';
    ctx.moveTo(pad.l, pad.t + ch);
    for (let i = 0; i < buckets.length; i++) {
      const x = pad.l + (i / (buckets.length - 1)) * cw;
      const y = pad.t + ch - (buckets[i][s.key] / maxVal) * ch;
      ctx.lineTo(x, y);
    }
    ctx.lineTo(pad.l + cw, pad.t + ch);
    ctx.closePath();
    ctx.fill();

    // Line
    ctx.beginPath();
    for (let i = 0; i < buckets.length; i++) {
      const x = pad.l + (i / (buckets.length - 1)) * cw;
      const y = pad.t + ch - (buckets[i][s.key] / maxVal) * ch;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();
  }
}

function renderCharts(buckets) {
  drawChart('chart-throughput', buckets, [
    { key: 'ok', color: '#3fb950' },
  ]);
  drawChart('chart-errors', buckets, [
    { key: 'errors', color: '#f85149' },
    { key: 'cf', color: '#39d2c0' },
  ]);
}

async function refresh() {
  try {
    const [pool, agents, metrics, timeseries, tidesData] = await Promise.all([
      fetchJSON('/pool/status'),
      fetchJSON('/dashboard/agents'),
      fetchText('/metrics'),
      fetchJSON('/dashboard/timeseries'),
      fetchJSON('/tides/status').catch(() => null),
    ]);
    renderStats(pool, agents);
    renderAgents(agents, pool);
    renderMetrics(metrics);
    renderCharts(timeseries);
    if (tidesData) {
      const el = document.getElementById('tides-stats');
      if (el) {
        const phaseColors = {high:'green',rising:'cyan',falling:'yellow',low:'purple'};
        const pc = phaseColors[tidesData.phase] || '';
        const secs = Math.round(tidesData.interval / 1e9);
        const boostList = Object.entries(tidesData.boosts || {}).map(([k,v]) => k+':'+v.toFixed(1)).join(' ') || 'none';
        el.innerHTML =
          '<div class="stat-sm ' + pc + '">' + secs + 's</div><div class="stat-label">interval</div>' +
          '<div style="margin-top:6px"><span class="tag tag-' + (tidesData.phase === 'high' ? 'available' : tidesData.phase === 'low' ? 'heavy' : 'leased') + '">' + tidesData.phase + ' tide</span></div>' +
          '<div style="margin-top:4px;font-size:10px;color:#484f58">boosts: ' + boostList + '</div>';
      }
    }
    document.getElementById('tick').textContent = new Date().toLocaleTimeString();
  } catch (e) {
    document.getElementById('tick').textContent = 'error: ' + e.message;
  }
}

refresh();
setInterval(refresh, 2000);
</script>
</body>
</html>`

func (s *Server) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	buckets := s.events.Buckets(5 * time.Second)
	writeJSON(w, http.StatusOK, buckets)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}

// dashboardAgents returns agent data enriched with state for the dashboard.
func (s *Server) handleDashboardAgents(w http.ResponseWriter, r *http.Request) {
	s.pool.mu.RLock()
	defer s.pool.mu.RUnlock()

	type agentView struct {
		ID       string                  `json:"id"`
		IP       string                  `json:"ip,omitempty"`
		Backend  string                  `json:"backend"`
		Class    string                  `json:"class"`
		State    string                  `json:"state"`
		UseCount int                     `json:"use_count"`
		Domains  map[string]interface{}  `json:"domains"`
	}

	out := make([]agentView, 0, len(s.pool.agents))
	for _, a := range s.pool.agents {
		// Marshal/unmarshal domains to get a clean interface{}
		var domains map[string]interface{}
		if domainsRaw, err := json.Marshal(a.Identity.Domains); err == nil {
			json.Unmarshal(domainsRaw, &domains)
		}

		out = append(out, agentView{
			ID:       a.Identity.ID,
			IP:       a.Identity.IP,
			Backend:  a.Backend,
			Class:    a.Class,
			State:    a.State,
			UseCount: a.Identity.UseCount,
			Domains:  domains,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
