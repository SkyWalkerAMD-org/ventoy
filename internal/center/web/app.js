'use strict';
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
let token = sessionStorage.getItem('ventoy-token') || '';
let data = { machines: [], jobs: [], images: [], packages: [] };
let armId = '';
let closeId = '';
let refreshInFlight = null;
const states = {draft:'草稿',ready:'等待网启',installing:'正在安装',enrolling:'配置接入中',enrolled:'接入已配置',installed:'系统已安装',failed:'失败',canceled:'已取消'};
const osNames = {rocky:'Rocky Linux 9',ubuntu:'Ubuntu Server 24.04',windows:'Windows'};
function el(tag, text, cls) { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; }
function notify(message) { const t = $('#toast'); t.textContent = message; t.hidden = false; clearTimeout(notify.timer); notify.timer = setTimeout(() => {t.hidden = true;}, 7000); }
async function api(path, options = {}) {
  const response = await fetch(path, {...options, headers:{Authorization:`Bearer ${token}`, ...(options.body ? {'Content-Type':'application/json'} : {}), ...options.headers}});
  if (!response.ok) { if (response.status === 401) showLogin(); const body = await response.json().catch(() => ({})); throw new Error(body.error || `请求失败 (${response.status})`); }
  return response;
}
function post(path, body = {}) { return api(path, {method:'POST', body:JSON.stringify(body)}); }
function showLogin() { if (!$('#login-dialog').open) $('#login-dialog').showModal(); }
function closeDialog(event) { event.target.closest('dialog').close(); }
$$('[data-close]').forEach(b => b.addEventListener('click', closeDialog));
$('#login-dialog').addEventListener('cancel', event => event.preventDefault());
function route() { const page = location.hash.slice(1) || 'overview'; const chosen = ['overview','machines','jobs','images'].includes(page) ? page : 'overview'; $$('.page').forEach(p => {p.hidden = p.id !== chosen;}); $$('[data-nav]').forEach(a => a.classList.toggle('active', a.dataset.nav === chosen)); $('#breadcrumb').textContent = {overview:'部署总览',machines:'目标机器',jobs:'安装任务',images:'镜像与接入'}[chosen]; }
window.addEventListener('hashchange', route); route();
function empty(target, title, description, action) { const box = el('div',null,'empty'); box.append(el('div','↗','empty-symbol'),el('h2',title),el('p',description)); if (action) {const b=el('button',action[0],'primary'); b.addEventListener('click',action[1]);box.append(b);} target.replaceChildren(box); }
function table(target, headings, rows) { const t=el('table'); const head=el('thead'); const tr=el('tr'); headings.forEach(h=>tr.append(el('th',h))); head.append(tr); const body=el('tbody'); rows.forEach(row=>{const r=el('tr'); row.forEach(cell=>{const c=el('td'); if(cell instanceof Node)c.append(cell);else c.textContent=cell; r.append(c);});body.append(r);});t.append(head,body);target.replaceChildren(t); }
function button(text, action, cls='') { const b=el('button',text,cls);b.addEventListener('click',async()=>{b.disabled=true;try{await action();}catch(error){notify(error.message);}finally{b.disabled=false;}});return b; }
function stamp(value) { return new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}); }
function renderJobs(target,jobs) {
  if(!jobs.length){empty(target,'还没有安装任务','登记第一台机器，准备镜像，然后创建安装草稿。',['新建安装任务',newJob]);return;}
  table(target,['目标机器','系统 / 镜像','状态','创建时间','操作'],jobs.map(job=>{
    const name=el('div',job.machine.name);name.append(el('small',job.machine.mac));const system=el('div',osNames[job.os]);system.append(el('small',job.iso));const actions=el('div',null,'actions');
    if(job.state==='draft')actions.append(button('授权安装',()=>openArm(job)));
    if(job.state==='ready')actions.append(button('下载安装包',()=>downloadBundle(job),'primary'));
    actions.append(button('记录',()=>details(job)));
    if(['installing','enrolling'].includes(job.state))actions.append(button('结束失败任务',()=>{closeId=job.id;$('#close-form').reset();$('#close-target').textContent='目标机器：'+job.machine.name;$('#close-dialog').showModal();}));
    if(['draft','ready'].includes(job.state))actions.append(button('取消',async()=>{await post(`/api/jobs/${job.id}/cancel`);await refresh();}));
    return [name,system,el('span',states[job.state]||job.state,`badge ${job.state}`),stamp(job.created),actions];
  }));
}
function renderAll() {
  $('#version').textContent=data.version;$('#count-machines').textContent=data.machines.length;$('#count-ready').textContent=data.jobs.filter(j=>j.state==='ready').length;$('#count-running').textContent=data.jobs.filter(j=>['installing','enrolling'].includes(j.state)).length;$('#count-done').textContent=data.jobs.filter(j=>['installed','enrolled'].includes(j.state)).length;
  renderJobs($('#recent-jobs'),data.jobs.slice(0,6));renderJobs($('#all-jobs'),data.jobs);
  if(!data.machines.length)empty($('#machine-table'),'登记第一台目标机器','准备启动网卡 MAC、资产编号和目标磁盘序列号。',['登记机器',newMachine]);
  else table($('#machine-table'),['节点名','启动网卡 MAC','资产编号','目标磁盘序列号','模式 / Windows 磁盘','操作'],data.machines.map(m=>[m.name,m.mac,m.serial,m.disk_serial,`${m.firmware.toUpperCase()} / Disk ${m.windows_disk}`,button('移除登记',async()=>{await api(`/api/machines/${m.id}`,{method:'DELETE'});await refresh();})]));
  for(const [selector,items] of [['#image-table',data.images||[]],['#package-table',data.packages||[]]]){if(!items.length)empty($(selector),'目录中暂无文件','在服务器放入对应文件后，点击右上角刷新。');else table($(selector),['文件','大小'],items.map(item=>[item.path,`${(item.size/1024/1024).toFixed(1)} MB`]));}
  for(const [id,url] of [['#iventoy-link',data.iventoy_url],['#fyhocsys-link',data.fyhocsys_url]]){const a=$(id);a.hidden=!url;if(url)a.href=url;}
  $('#warnings').hidden=!data.warnings.length;$('#warnings').textContent=data.warnings.join('；');
}
function refresh(force = false) {
  if (!token) return Promise.resolve();
  if (refreshInFlight) return force ? refreshInFlight.catch(() => {}).then(() => refresh(true)) : refreshInFlight;
  refreshInFlight = (async () => {
    try { data = await (await api('/api/overview')).json(); renderAll(); $('#connection-state').textContent = '管理端已连接 · ' + stamp(Date.now()); }
    catch (error) { $('#connection-state').textContent = '连接中断'; throw error; }
    finally { refreshInFlight = null; }
  })();
  return refreshInFlight;
}
function newMachine(){ $('#machine-form').reset();$('#machine-dialog').showModal(); }
function setOptions(select,items,label,value){select.replaceChildren();const p=el('option','请选择');p.value='';select.append(p);items.forEach(item=>{const o=el('option',label(item));o.value=value(item);select.append(o);});}
function newJob(){if(!data.machines.length){notify('请先登记一台目标机器');newMachine();return;}if(!data.images?.length){notify('请先把 ISO 放入服务器镜像目录并刷新');location.hash='images';return;}const f=$('#job-form');f.reset();setOptions(f.elements.machine_id,data.machines,m=>`${m.name} · ${m.mac}`,m=>m.id);setOptions(f.elements.iso,data.images,i=>i.path,i=>i.path);updateJobFields();$('#job-dialog').showModal();}
function updateJobFields(){const f=$('#job-form');const win=f.elements.os.value==='windows';$$('[data-windows]',f).forEach(n=>n.hidden=!win);$$('[data-linux]',f).forEach(n=>n.hidden=win);f.elements.password.required=win;f.elements.password_hash.required=!win;f.elements.enroll.disabled=win;if(win)f.elements.enroll.checked=false;const enroll=f.elements.enroll.checked&&!win;$('#enroll-fields').hidden=!enroll;f.elements.package.required=enroll;f.elements.connection.required=enroll;const ext=f.elements.os.value==='ubuntu'?'.deb':'.rpm';setOptions(f.elements.package,(data.packages||[]).filter(p=>p.path.endsWith(ext)),p=>p.path,p=>p.path);}
$('#job-form').elements.os.addEventListener('change',updateJobFields);$('#job-form').elements.enroll.addEventListener('change',updateJobFields);
$$('[data-action]').forEach(b=>b.addEventListener('click',()=>b.dataset.action==='new-machine'?newMachine():newJob()));
async function submit(form,action){const b=$('button[type=submit]',form)||$('button.primary:last-child',form)||$('button.danger',form);if(b)b.disabled=true;try{await action();await refresh(true);form.closest('dialog').close();}catch(error){notify(error.message);}finally{if(b)b.disabled=false;}}
$('#machine-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const body=Object.fromEntries(new FormData(f));body.windows_disk=Number(body.windows_disk);await post('/api/machines',body);notify('目标机器已登记');});});
$('#job-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const form=new FormData(f);const enroll=f.elements.enroll.checked&&form.get('os')!=='windows';const body={machine_id:form.get('machine_id'),os:form.get('os'),iso:form.get('iso'),username:form.get('username'),password:form.get('password'),password_hash:form.get('password_hash'),ssh_key:form.get('ssh_key'),windows_image:Number(form.get('windows_image')),locale:form.get('locale'),enroll,package:enroll?form.get('package'):''};if(enroll){const file=f.elements.connection.files[0];if(!file||file.size>128*1024)throw new Error('连接文件缺失或超过 128 KB');body.connection=JSON.parse(await file.text());}await post('/api/jobs',body);notify('草稿已保存；授权后可生成安装包');});});
function openArm(job){armId=job.id;$('#arm-form').reset();$('#arm-target').textContent=`${job.machine.name} · ${job.machine.mac} · 磁盘 ${job.machine.disk_serial}`;$('#arm-dialog').showModal();}
$('#arm-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${armId}/arm`,{name:f.elements.name.value,disk_serial:f.elements.disk_serial.value,erase:f.elements.erase.checked});notify('已授权；下载并配置安装包后，从目标机器网卡启动');});});
async function downloadBundle(job){const response=await api(`/api/jobs/${job.id}/bundle`);const url=URL.createObjectURL(await response.blob());const a=el('a');a.href=url;a.download=`ventoy-${job.machine.name}-${job.id}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('安装包已下载，请按包内 README 配置 iVentoy');}
function details(job){const root=$('#job-detail');const dl=el('dl',null,'detail-grid');for(const [key,value] of [['任务编号',job.id],['节点',job.machine.name],['目标磁盘',job.machine.disk_serial],['状态',states[job.state]],['到期时间',job.state==='draft'?'尚未授权':stamp(job.expires)],['fyhocsys',job.enroll?'自动配置 Linux 接入':'未配置']]){dl.append(el('dt',key),el('dd',value));}const timeline=el('ol',null,'timeline');job.events.forEach(e=>{const li=el('li');li.append(el('strong',states[e.state]),el('small',` · ${stamp(e.time)}`));if(e.message)li.append(el('p',e.message));timeline.append(li);});root.replaceChildren(dl,timeline);if(job.state==='enrolled')root.append(el('p','本状态证明连接文件已导入且本地服务已启动。请在 fyhocsys 中核对新的节点心跳。','warning'));$('#detail-dialog').showModal();}
$('#login-form').addEventListener('submit',async event=>{event.preventDefault();token=event.target.elements.token.value.trim();try{await refresh();sessionStorage.setItem('ventoy-token',token);$('#login-dialog').close();$('#login-error').textContent='';event.target.reset();}catch(error){$('#login-error').textContent=error.message;}});
$('#logout').addEventListener('click',()=>{token='';sessionStorage.removeItem('ventoy-token');location.reload();});$('#refresh').addEventListener('click',()=>refresh().catch(e=>notify(e.message)));
setInterval(()=>{if(!document.hidden&&token)refresh().catch(()=>{});},10000);
if(token)refresh().catch(e=>notify(e.message));else showLogin();

$('#close-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${closeId}/close`,{name:f.elements.name.value,stopped:f.elements.stopped.checked,reason:f.elements.reason.value});notify('任务已结单，旧授权已失效；重新安装请创建新任务');});});
