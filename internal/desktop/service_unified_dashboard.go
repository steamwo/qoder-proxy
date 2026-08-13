package desktop

const unifiedDashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Qoder Proxy</title>
<style>
:root{font-family:Inter,"Segoe UI",system-ui,-apple-system,sans-serif;color:#161b24;background:#f5f7fa;color-scheme:light}*{box-sizing:border-box}body{margin:0;min-height:100vh}.shell{max-width:1160px;margin:0 auto;padding:30px 22px 56px}.top{display:flex;justify-content:space-between;align-items:flex-start;gap:20px}.brand{font-size:29px;font-weight:760;letter-spacing:-.02em}.sub{font-size:13px;color:#667085;margin-top:5px}.state{display:inline-flex;align-items:center;gap:8px;padding:8px 12px;border-radius:999px;background:#eef1f5;color:#667085;font-size:13px;font-weight:700}.state:before{content:"";width:8px;height:8px;border-radius:50%;background:#98a2b3}.state.on{background:#e8f7f1;color:#087c61}.state.on:before{background:#12a47f}.tabs{display:flex;gap:7px;margin:20px 0 14px;padding:4px;background:#edf1f5;border-radius:12px;width:max-content}.tab{border:0;background:transparent;color:#667085;border-radius:9px;padding:8px 14px;font:inherit;font-size:13px;font-weight:650;cursor:pointer}.tab.active{background:#fff;color:#1d2939;box-shadow:0 1px 4px rgba(16,24,40,.08)}.panel{display:none}.panel.active{display:block}.card{background:#fff;border:1px solid #e1e6ed;border-radius:18px;box-shadow:0 7px 28px rgba(31,41,55,.045);padding:22px;margin-bottom:16px}.status-card{padding:0;overflow:hidden}.status-head{display:flex;justify-content:space-between;gap:20px;padding:22px 24px 18px;border-bottom:1px solid #edf0f4}.status-title{font-size:17px;font-weight:720}.status-note{font-size:12px;color:#8a94a6;margin-top:4px}.section{padding:19px 24px;border-bottom:1px solid #edf0f4}.section:last-child{border-bottom:0}.section-title{font-size:11px;font-weight:750;color:#8a94a6;text-transform:uppercase;letter-spacing:.06em;margin-bottom:12px}.facts{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.fact{background:#f7f9fb;border:1px solid #edf0f4;border-radius:12px;padding:13px 14px;min-height:73px}.k{font-size:11px;color:#8a94a6;margin-bottom:6px}.v{font-size:15px;font-weight:700;overflow-wrap:anywhere}.v.big{font-size:17px}.muted{font-size:12px;color:#667085}.endpoint{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:13px}.apis{display:flex;gap:7px;flex-wrap:wrap;margin-top:12px}.api{padding:6px 9px;border:1px solid #dce3eb;border-radius:8px;background:#fff;font:11px ui-monospace,SFMono-Regular,Consolas,monospace;color:#475467}.account-grid{display:grid;grid-template-columns:1.2fr .8fr .8fr .8fr;gap:10px}.quota-bar{height:7px;background:#e8edf3;border-radius:999px;overflow:hidden;margin-top:8px}.quota-fill{height:100%;width:0;background:#1769e8;border-radius:999px;transition:width .2s}.actions{display:flex;gap:9px;flex-wrap:wrap}.btn{border:0;border-radius:10px;padding:9px 14px;font:inherit;font-size:13px;font-weight:680;cursor:pointer;background:#edf1f5;color:#344054}.btn:hover{filter:brightness(.985)}.btn.primary{background:#1769e8;color:#fff}.btn.danger{background:#fff0ef;color:#c23c36}.btn.ghost-danger{background:#fff;color:#b42318;border:1px solid #f1cbc8}.btn:disabled{opacity:.45;cursor:not-allowed}.inline-actions{display:flex;gap:7px;flex-wrap:wrap;margin-top:12px}.small{padding:7px 10px;font-size:12px}.error{display:none;color:#b42318;background:#fff3f2;border:1px solid #ffd4d0;border-radius:10px;padding:9px 11px;font-size:12px;margin-top:12px}.form{display:grid;grid-template-columns:190px 1fr;gap:13px 16px;align-items:center}.form label{font-size:13px;color:#667085}.form input{width:100%;padding:10px 11px;border:1px solid #d7dee8;border-radius:10px;font:inherit;background:#fff}.check{display:flex;align-items:center;gap:8px}.check input{width:auto}.notice{font-size:12px;color:#8a94a6;margin-top:10px}.models{width:100%;border-collapse:collapse;font-size:13px}.models th,.models td{text-align:left;padding:11px 9px;border-bottom:1px solid #edf0f4}.models th{color:#8a94a6;font-size:11px}.models select{padding:7px;border:1px solid #d7dee8;border-radius:9px;background:#fff}.log-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:12px}.log-stats{display:flex;gap:14px;flex-wrap:wrap;font-size:12px;color:#667085}.logbox{height:360px;overflow:auto;background:#10151d;color:#d0d7e2;border-radius:13px;padding:14px;font:12px/1.58 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap}.toggle{display:flex;align-items:center;gap:7px;font-size:12px;color:#667085}.empty{color:#8a94a6}.row-between{display:flex;justify-content:space-between;align-items:flex-start;gap:12px}@media(max-width:900px){.facts,.account-grid{grid-template-columns:1fr 1fr}}@media(max-width:650px){.shell{padding:20px 12px 40px}.top{flex-direction:column}.tabs{width:100%;overflow:auto}.facts,.account-grid{grid-template-columns:1fr}.form{grid-template-columns:1fr}.status-head,.section{padding-left:16px;padding-right:16px}.log-toolbar{align-items:flex-start;flex-direction:column}}
</style>
</head>
<body>
<div class="shell">
  <div class="top">
    <div><div class="brand">Qoder Proxy</div><div class="sub">轻量后台服务 · API 与管理界面共用一个端口</div></div>
    <div id="state" class="state">读取状态…</div>
  </div>

  <div class="tabs">
    <button class="tab active" data-tab="overview">状态</button>
    <button class="tab" data-tab="settings">配置</button>
    <button class="tab" data-tab="models">模型</button>
    <button class="tab" data-tab="logs">日志</button>
  </div>

  <section id="overview" class="panel active">
    <div class="card status-card">
      <div class="status-head">
        <div><div class="status-title">服务状态</div><div class="status-note">关闭浏览器不会停止服务；只有“退出服务”才会结束后台进程。</div></div>
      </div>

      <div class="section">
        <div class="section-title">端点与接口</div>
        <div class="facts">
          <div class="fact"><div class="k">暴露端点</div><div id="endpoint" class="v big endpoint">—</div></div>
          <div class="fact"><div class="k">运行时长</div><div id="uptime" class="v big">—</div></div>
          <div class="fact"><div class="k">Runtime Sys</div><div id="memory" class="v big">—</div></div>
          <div class="fact"><div class="k">Go Heap</div><div id="heap" class="v big">—</div></div>
        </div>
        <div class="apis">
          <span class="api">GET /v1/models</span>
          <span class="api">POST /v1/chat/completions</span>
          <span class="api">POST /v1/responses</span>
          <span class="api">POST /v1/messages</span>
        </div>
      </div>

      <div class="section">
        <div class="section-title">账号与额度</div>
        <div class="account-grid">
          <div class="fact"><div class="k">Qoder 账号</div><div id="account" class="v">未授权</div><div id="accountName" class="muted">—</div></div>
          <div class="fact"><div class="k">套餐</div><div id="plan" class="v">—</div><div id="quotaFetched" class="muted">—</div></div>
          <div class="fact"><div class="k">个人额度剩余</div><div id="quotaPercent" class="v">—</div><div class="quota-bar"><div id="quotaFill" class="quota-fill"></div></div></div>
          <div class="fact"><div class="k">额度重置</div><div id="quotaReset" class="v">—</div><div id="quotaRaw" class="muted">—</div></div>
        </div>
        <div class="inline-actions">
          <button id="login" class="btn primary small" onclick="startLogin()">登录 / 重新授权</button>
          <button class="btn small" onclick="refreshQuota()">刷新额度</button>
        </div>
        <div id="authHint" class="notice">可直接从网页发起 Qoder 授权。</div>
      </div>

      <div class="section">
        <div class="section-title">服务操作</div>
        <div class="actions">
          <button id="start" class="btn primary" onclick="action('start')">启动代理</button>
          <button id="stop" class="btn" onclick="action('stop')">停止代理</button>
          <button class="btn" onclick="action('reload')">重载配置</button>
          <button id="logout" class="btn ghost-danger" onclick="logout()">退出账号</button>
          <button class="btn danger" onclick="shutdownService()">退出服务</button>
        </div>
        <div id="overviewError" class="error"></div>
      </div>
    </div>
  </section>

  <section id="settings" class="panel">
    <div class="card">
      <div class="row-between"><div><div class="status-title">代理配置</div><div class="notice">保存后自动重载可热更新配置；监听地址变化需要退出服务后重新启动。</div></div></div>
      <div class="form" style="margin-top:18px">
        <label>监听地址</label><input id="listen" placeholder="127.0.0.1:9000">
        <label>本地 API Key</label><input id="apiKey" type="password" placeholder="留空则不要求本地鉴权">
        <label>队列重试次数</label><input id="queueRetries" type="number" min="0">
        <label>最大排队时间</label><input id="queueMaxWait" placeholder="10m">
        <label>服务启动后自动启用代理</label><div class="check"><input id="autoStart" type="checkbox"><span class="muted">启用</span></div>
      </div>
      <div class="actions" style="margin-top:18px"><button class="btn primary" onclick="saveSettings()">保存配置</button></div>
      <div id="settingsError" class="error"></div>
    </div>
  </section>

  <section id="models" class="panel">
    <div class="card">
      <div class="status-title">模型默认思考深度</div>
      <div class="notice" style="margin-bottom:14px">只展示对应模型实时 Qoder thinking_config 支持的选项；“自动”表示不覆盖上游默认行为。</div>
      <div id="modelArea" class="muted">打开此页后加载模型…</div>
    </div>
  </section>

  <section id="logs" class="panel">
    <div class="card">
      <div class="log-toolbar">
        <div><div class="status-title">运行日志</div><div id="logStats" class="log-stats"><span>请求 —</span><span>错误 —</span><span>平均耗时 —</span></div></div>
        <div class="actions">
          <label class="toggle"><input id="logAuto" type="checkbox" checked> 自动刷新</label>
          <button class="btn small" onclick="refreshLogs(true)">立即刷新</button>
          <button class="btn ghost-danger small" onclick="clearLogs()">清空日志</button>
        </div>
      </div>
      <div id="logError" class="error"></div>
      <div id="logbox" class="logbox">读取日志…</div>
    </div>
  </section>
</div>
<script>
var busy=false;
var currentTab='overview';
function duration(sec){sec=Math.max(0,Number(sec)||0);var h=Math.floor(sec/3600),m=Math.floor(sec%3600/60),ss=Math.floor(sec%60);return h?(h+'h '+m+'m'):(m?(m+'m '+ss+'s'):(ss+'s'));}
function err(id,text){var e=document.getElementById(id);e.textContent=text||'';e.style.display=text?'block':'none';}
function fmtDate(v){if(!v)return '—';var d=new Date(v);if(isNaN(d.getTime()))return v;return d.toLocaleString();}
function fmtNum(v){var n=Number(v);if(!isFinite(n))return '—';return n.toLocaleString(undefined,{maximumFractionDigits:1});}
async function jsonFetch(url,opt){var r=await fetch(url,opt||{cache:'no-store'});var j=await r.json();if(!r.ok)throw new Error(j.error||('HTTP '+r.status));return j;}
async function refresh(){try{var s=await jsonFetch('/admin/api/status');var st=document.getElementById('state');st.className='state'+(s.running?' on':'');st.textContent=s.running?'代理运行中':'代理已停止';var ep=s.proxy_address||s.configured_listen||'—';document.getElementById('endpoint').textContent=(ep==='—'?ep:'http://'+ep)+(s.restart_required?' · 端口待重启':'');document.getElementById('uptime').textContent=s.running?duration(s.uptime_seconds):'—';document.getElementById('memory').textContent=s.runtime_sys_mb+' MB';document.getElementById('heap').textContent=s.heap_alloc_mb+' MB';document.getElementById('start').disabled=busy||s.running;document.getElementById('stop').disabled=busy||!s.running;document.getElementById('logout').disabled=busy||!s.credential_ready;document.getElementById('account').textContent=s.credential_email||(s.credential_ready?'已授权':'未授权');document.getElementById('accountName').textContent=s.credential_name||(!s.credential_ready?'请先完成 Qoder 授权':'');if(!s.credential_ready){resetQuota('未授权');}}catch(e){err('overviewError',e.message||String(e));}}
function resetQuota(text){document.getElementById('plan').textContent=text||'—';document.getElementById('quotaPercent').textContent='—';document.getElementById('quotaFill').style.width='0%';document.getElementById('quotaReset').textContent='—';document.getElementById('quotaRaw').textContent='—';document.getElementById('quotaFetched').textContent='—';}
async function refreshQuota(){try{var q=await jsonFetch('/admin/api/quota');document.getElementById('plan').textContent=q.plan||'Qoder';var p=Number(q.user_remaining_percent);document.getElementById('quotaPercent').textContent=isFinite(p)?(p.toFixed(1)+'%'):'—';document.getElementById('quotaFill').style.width=(isFinite(p)?Math.max(0,Math.min(100,p)):0)+'%';document.getElementById('quotaReset').textContent=fmtDate(q.reset_at);document.getElementById('quotaRaw').textContent=(Number(q.user_limit)>0)?(fmtNum(q.user_remaining)+' / '+fmtNum(q.user_limit)):'—';document.getElementById('quotaFetched').textContent=q.fetched_at?('更新 '+fmtDate(q.fetched_at)):'—';}catch(e){resetQuota('额度获取失败');document.getElementById('quotaFetched').textContent=e.message||String(e);}}
async function loadSettings(){try{var s=await jsonFetch('/admin/api/settings');document.getElementById('listen').value=s.listen||'';document.getElementById('apiKey').value=s.api_key||'';document.getElementById('queueRetries').value=s.queue_retries;document.getElementById('queueMaxWait').value=s.queue_max_wait||'';document.getElementById('autoStart').checked=!!s.auto_start;}catch(e){err('settingsError',e.message||String(e));}}
async function saveSettings(){err('settingsError','');try{var body={listen:document.getElementById('listen').value,api_key:document.getElementById('apiKey').value,queue_retries:Number(document.getElementById('queueRetries').value),queue_max_wait:document.getElementById('queueMaxWait').value,auto_start:document.getElementById('autoStart').checked};await jsonFetch('/admin/api/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});await refresh();}catch(e){err('settingsError',e.message||String(e));}}
async function action(name){busy=true;err('overviewError','');try{await jsonFetch('/admin/api/'+name,{method:'POST'});}catch(e){err('overviewError',e.message||String(e));}finally{busy=false;await refresh();await refreshLogs(false);}}
async function startLogin(){err('overviewError','');try{var s=await jsonFetch('/admin/api/login/start',{method:'POST'});if(s.url)window.open(s.url,'_blank','noopener');document.getElementById('authHint').textContent='已打开 Qoder 授权页，完成后会自动更新账号和额度。';pollLogin();}catch(e){err('overviewError',e.message||String(e));}}
async function pollLogin(){for(var i=0;i<150;i++){await new Promise(function(resolve){setTimeout(resolve,2000);});try{var s=await jsonFetch('/admin/api/login/status');if(!s.pending){if(s.error){err('overviewError',s.error);}else{document.getElementById('authHint').textContent='授权成功';await refresh();await refreshQuota();await loadModels();}return;}}catch(e){return;}}}
async function logout(){if(!confirm('退出 Qoder 账号？当前代理会停止，并删除本机保存的 Qoder 凭据。'))return;busy=true;try{await jsonFetch('/admin/api/logout',{method:'POST'});document.getElementById('authHint').textContent='账号已退出';await refresh();resetQuota('未授权');}catch(e){err('overviewError',e.message||String(e));}finally{busy=false;}}
async function refreshLogs(force){if(!force&&currentTab!=='logs')return;if(!force&&!document.getElementById('logAuto').checked)return;try{err('logError','');var data=await jsonFetch('/admin/api/logs');var b=document.getElementById('logbox');var near=b.scrollHeight-b.scrollTop-b.clientHeight<60;b.textContent=data.text||'暂无日志。代理请求、授权、配置变更和服务事件会显示在这里。';document.getElementById('logStats').innerHTML='<span>请求 '+data.requests+'</span><span>错误 '+data.errors+'</span><span>平均耗时 '+data.average_millis+' ms</span>';if(near)b.scrollTop=b.scrollHeight;}catch(e){err('logError',e.message||String(e));}}
async function clearLogs(){if(!confirm('清空内存和磁盘中的本地日志？'))return;try{await jsonFetch('/admin/api/logs/clear',{method:'POST'});await refreshLogs(true);}catch(e){err('logError',e.message||String(e));}}
function effortLabel(v){if(v==='')return '自动';if(v==='none')return '关闭';if(v==='minimal')return '最小';if(v==='low')return '低';if(v==='medium')return '中';if(v==='high')return '高';if(v==='xhigh'||v==='max')return '最高';return v;}
function escapeHTML(s){return String(s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
function escapeAttr(s){return escapeHTML(s);}
async function loadModels(){var area=document.getElementById('modelArea');area.textContent='正在加载模型…';try{var models=await jsonFetch('/admin/api/models');var html='<table class="models"><thead><tr><th>模型</th><th>上下文上限</th><th>输出上限</th><th>默认思考</th></tr></thead><tbody>';models.forEach(function(m){html+='<tr><td>'+escapeHTML(m.display_name)+'</td><td>'+m.max_input_tokens+'</td><td>'+m.max_output_tokens+'</td><td><select data-model="'+escapeAttr(m.upstream_id)+'" onchange="saveModelDefault(this)">';m.options.forEach(function(o){html+='<option value="'+escapeAttr(o)+'"'+(o===m.default?' selected':'')+'>'+escapeHTML(effortLabel(o))+'</option>';});html+='</select></td></tr>';});html+='</tbody></table>';area.innerHTML=html;}catch(e){area.textContent=e.message||String(e);}}
async function saveModelDefault(sel){try{await jsonFetch('/admin/api/models/default',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({model_id:sel.getAttribute('data-model'),effort:sel.value})});}catch(e){alert(e.message||String(e));await loadModels();}}
async function shutdownService(){if(!confirm('退出 Qoder Proxy 后台服务？代理会立即停止。'))return;try{await jsonFetch('/admin/api/shutdown',{method:'POST'});document.getElementById('state').className='state';document.getElementById('state').textContent='服务已退出';document.querySelectorAll('button').forEach(function(b){b.disabled=true;});}catch(e){err('overviewError',e.message||String(e));}}
document.querySelectorAll('.tab').forEach(function(btn){btn.addEventListener('click',function(){document.querySelectorAll('.tab').forEach(function(x){x.classList.remove('active');});document.querySelectorAll('.panel').forEach(function(x){x.classList.remove('active');});btn.classList.add('active');currentTab=btn.getAttribute('data-tab');document.getElementById(currentTab).classList.add('active');if(currentTab==='settings')loadSettings();if(currentTab==='models')loadModels();if(currentTab==='logs')refreshLogs(true);});});
refresh();loadSettings();refreshQuota();setInterval(refresh,2500);setInterval(function(){refreshLogs(false);},2500);setInterval(function(){if(currentTab==='overview')refreshQuota();},60000);
</script>
</body>
</html>`
