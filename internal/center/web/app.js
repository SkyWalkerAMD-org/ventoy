'use strict';
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
let token = sessionStorage.getItem('ventoy-token') || '';
let data = { machines: [], jobs: [], images: [], profiles: [] };
let selectedImage = '';
let armId = '';
let closeId = '';
let refreshInFlight = null;
const states = {draft:'草稿',ready:'等待网启',installing:'正在安装',enrolling:'配置接入中',enrolled:'接入已配置',installed:'系统已安装',failed:'失败',canceled:'已取消'};
const osNames = {centos:'CentOS',rocky:'Rocky Linux',ubuntu:'Ubuntu Server',debian:'Debian',windows:'Windows'};
const profileFor = id => data.profiles.find(p => p.id === id);
function jobSystem(job) { return profileFor(job.profile)?.label || osNames[job.os] || job.os; }
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
function route() { const page = location.hash.slice(1) || 'overview'; const chosen = ['overview','machines','jobs','images','profiles'].includes(page) ? page : 'overview'; $$('.page').forEach(p => {p.hidden = p.id !== chosen;}); $$('[data-nav]').forEach(a => a.classList.toggle('active', a.dataset.nav === chosen)); $('#breadcrumb').textContent = {overview:'部署总览',machines:'目标机器',jobs:'安装任务',images:'镜像管理',profiles:'系统版本'}[chosen]; }
window.addEventListener('hashchange', route); route();
function empty(target, title, description, action) { const box = el('div',null,'empty'); box.append(el('div','↗','empty-symbol'),el('h2',title),el('p',description)); if (action) {const b=el('button',action[0],'primary'); b.addEventListener('click',action[1]);box.append(b);} target.replaceChildren(box); }
function table(target, headings, rows) { const t=el('table'); const head=el('thead'); const tr=el('tr'); headings.forEach(h=>tr.append(el('th',h))); head.append(tr); const body=el('tbody'); rows.forEach(row=>{const r=el('tr'); row.forEach(cell=>{const c=el('td'); if(cell instanceof Node)c.append(cell);else c.textContent=cell; r.append(c);});body.append(r);});t.append(head,body);target.replaceChildren(t); }
function button(text, action, cls='') { const b=el('button',text,cls);b.addEventListener('click',async()=>{b.disabled=true;try{await action();}catch(error){notify(error.message);}finally{b.disabled=false;}});return b; }
function stamp(value) { return new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}); }
function renderJobs(target,jobs) {
  if(!jobs.length){empty(target,'还没有安装任务','登记第一台机器，准备镜像，然后创建安装草稿。',['新建安装任务',newJob]);return;}
  table(target,['目标机器','系统 / 镜像','状态','创建时间','操作'],jobs.map(job=>{
    const name=el('div',job.machine.name);name.append(el('small',job.machine.mac));const system=el('div',jobSystem(job));system.append(el('small',job.iso));const actions=el('div',null,'actions');
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
  renderMachines(); renderBoot(); renderImages(); renderProfiles();
  const link=$('#iventoy-link');link.hidden=!data.iventoy_url;if(data.iventoy_url)link.href=data.iventoy_url;
  $('#warnings').hidden=!data.warnings.length;$('#warnings').textContent=data.warnings.join('；');
}
function renderMachines() {
  const search=$('#machine-search').value.toLowerCase().replace(/[:-]/g,'');
  const machines=data.machines.filter(m=>[m.name,m.mac,m.serial].some(value=>value.toLowerCase().replace(/[:-]/g,'').includes(search)));
  if(!machines.length){empty($('#machine-table'),search?'没有匹配的机器':'登记第一台目标机器',search?'尝试其他节点名、MAC 或资产编号。':'准备启动网卡 MAC、资产编号和目标磁盘序列号。',search?null:['登记机器',newMachine]);return;}
  table($('#machine-table'),['节点名','启动网卡 MAC','资产编号','目标磁盘序列号','启动模式','操作'],machines.map(m=>[m.name,m.mac,m.serial,m.disk_serial,m.firmware.toUpperCase(),button('移除登记',async()=>{await api(`/api/machines/${m.id}`,{method:'DELETE'});await refresh();})]));
}
function renderBoot() {
  if(!data.machines.length){empty($('#boot-table'),'等待第一台机器','登记机器并创建任务后，在这里查看启动模式和安装状态。');return;}
  table($('#boot-table'),['节点 / MAC','启动模式','安装镜像','最近状态','操作'],data.machines.map(m=>{
    const job=data.jobs.find(j=>j.machine.id===m.id);const identity=el('div',m.name);identity.append(el('small',m.mac));
    return [identity,m.firmware.toUpperCase(),job?.iso||'尚未分配',el('span',job?states[job.state]:'未创建任务',`badge ${job?.state||''}`),job?button('详情',()=>details(job)):'—'];
  }));
}
function renderProfiles() {
  const root=$('#profile-list');root.replaceChildren();
  for(const os of ['rocky','centos','ubuntu','debian','windows']) {
    const card=el('article',null,'panel padded');card.append(el('div',os==='windows'?'次要支持':'LINUX / X86-64','label'),el('h2',osNames[os]));
    const list=el('ul',null,'profile-versions');
    data.profiles.filter(p=>p.os===os).forEach(p=>{const item=el('li');item.append(el('strong',p.label),el('small',`${p.installer} · ${p.media}`),el('p',p.notes));list.append(item);});
    card.append(list);root.append(card);
  }
}
function renderImages() {
  const tree=$('#image-tree');const opened=new Map($$('details',tree).map(d=>[d.dataset.path,d.open]));
  const query=$('#image-search').value.toLowerCase();const items=(data.images||[]).filter(i=>i.path.toLowerCase().includes(query));
  $('#image-count').textContent=`${items.length} 个 ISO`;
  if(!items.length)empty(tree,'没有匹配的镜像',query?'尝试其他文件名。':'将 ISO 放入服务器 iso_dir 后刷新。');
  else {
    tree.replaceChildren();const folders=new Map([['',tree]]);
    for(const item of items){const parts=item.path.split('/');let parent=tree;let path='';
      for(const segment of parts.slice(0,-1)){path=path?`${path}/${segment}`:segment;if(!folders.has(path)){const folder=el('details',null,'media-folder');folder.dataset.path=path;folder.open=query?true:(opened.get(path)??true);folder.append(el('summary',segment));const children=el('div');folder.append(children);parent.append(folder);folders.set(path,children);}parent=folders.get(path);}
      const b=button(parts.at(-1),()=>{selectedImage=item.path;renderImages();},'media-file');b.classList.toggle('selected',item.path===selectedImage);b.setAttribute('aria-pressed',String(item.path===selectedImage));parent.append(b);
    }
  }
  const detail=$('#image-detail');const item=data.images?.find(i=>i.path===selectedImage);
  if(!item){empty(detail,'选择一个镜像','查看镜像信息，为指定机器创建安装任务。');return;}
  const profile=profileFor(item.suggested_profile);const dl=el('dl',null,'detail-grid');
  for(const [label,value] of [['目录路径',item.path],['文件大小',`${(item.size/1024/1024/1024).toFixed(2)} GiB`],['版本建议',profile?.label||'未识别，创建任务时手动选择'],['安装方式',profile?.installer||'根据所选系统版本生成']])dl.append(el('dt',label),el('dd',value));
  detail.replaceChildren(el('div','ISO IMAGE','label'),el('h2',item.path.split('/').at(-1)),dl,el('p','版本建议来自文件名，请核对真实镜像内容和架构。'),button('使用此镜像创建任务',()=>newJob(item.path),'primary'));
}
$('#machine-search').addEventListener('input',renderMachines);
$('#image-search').addEventListener('input',renderImages);
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
function newJob(imagePath=''){if(!data.machines.length){notify('请先登记一台目标机器');newMachine();return;}if(!data.images?.length){notify('请先把 ISO 放入服务器镜像目录并刷新');location.hash='images';return;}const f=$('#job-form');f.reset();setOptions(f.elements.machine_id,data.machines,m=>`${m.name} · ${m.mac}`,m=>m.id);setOptions(f.elements.profile,data.profiles,p=>p.label,p=>p.id);f.elements.profile.value='rocky-9';setOptions(f.elements.iso,data.images,i=>i.path,i=>i.path);if(typeof imagePath==='string'&&imagePath){f.elements.iso.value=imagePath;const image=data.images.find(i=>i.path===imagePath);if(image?.suggested_profile)f.elements.profile.value=image.suggested_profile;}updateJobFields();$('#job-dialog').showModal();}
function updateJobFields(){const f=$('#job-form');const profile=profileFor(f.elements.profile.value);const win=profile?.os==='windows';$$('[data-windows]',f).forEach(n=>n.hidden=!win);$$('[data-linux]',f).forEach(n=>n.hidden=win);f.elements.password.required=win;f.elements.password_hash.required=!win;$('#profile-hint').textContent=profile?`${profile.media}。${profile.notes}`:'请选择目标系统版本。';}
$('#job-form').elements.profile.addEventListener('change',updateJobFields);
$('#job-form').elements.iso.addEventListener('change',()=>{const f=$('#job-form');const image=data.images.find(i=>i.path===f.elements.iso.value);if(image?.suggested_profile)f.elements.profile.value=image.suggested_profile;updateJobFields();});
$$('[data-action]').forEach(b=>b.addEventListener('click',()=>b.dataset.action==='new-machine'?newMachine():newJob()));
async function submit(form,action){const b=$('button[type=submit]',form)||$('button.primary:last-child',form)||$('button.danger',form);if(b)b.disabled=true;try{await action();await refresh(true);form.closest('dialog').close();}catch(error){notify(error.message);}finally{if(b)b.disabled=false;}}
$('#machine-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const body=Object.fromEntries(new FormData(f));body.windows_disk=Number(body.windows_disk);await post('/api/machines',body);notify('目标机器已登记');});});
$('#job-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const form=new FormData(f);const profile=profileFor(form.get('profile'));if(!profile)throw new Error('请选择系统版本');const win=profile.os==='windows';const body={machine_id:form.get('machine_id'),profile:profile.id,os:profile.os,iso:form.get('iso'),username:form.get('username'),password:win?form.get('password'):'',password_hash:win?'':form.get('password_hash'),ssh_key:win?'':form.get('ssh_key'),windows_image:Number(form.get('windows_image')),locale:form.get('locale'),enroll:false};await post('/api/jobs',body);notify('草稿已保存；授权后可生成安装包');});});
function openArm(job){armId=job.id;$('#arm-form').reset();$('#arm-target').textContent=`${job.machine.name} · ${job.machine.mac} · 磁盘 ${job.machine.disk_serial}`;$('#arm-dialog').showModal();}
$('#arm-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${armId}/arm`,{name:f.elements.name.value,disk_serial:f.elements.disk_serial.value,erase:f.elements.erase.checked});notify('已授权；下载并配置安装包后，从目标机器网卡启动');});});
async function downloadBundle(job){const response=await api(`/api/jobs/${job.id}/bundle`);const url=URL.createObjectURL(await response.blob());const a=el('a');a.href=url;a.download=`ventoy-${job.machine.name}-${job.id}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('安装包已下载，请按包内 README 配置 iVentoy');}
function details(job){const root=$('#job-detail');const dl=el('dl',null,'detail-grid');for(const [key,value] of [['任务编号',job.id],['节点',job.machine.name],['系统版本',jobSystem(job)],['目标磁盘',job.machine.disk_serial],['状态',states[job.state]],['到期时间',job.state==='draft'?'尚未授权':stamp(job.expires)]]){dl.append(el('dt',key),el('dd',value));}const timeline=el('ol',null,'timeline');job.events.forEach(e=>{const li=el('li');li.append(el('strong',states[e.state]),el('small',` · ${stamp(e.time)}`));if(e.message)li.append(el('p',e.message));timeline.append(li);});root.replaceChildren(dl,timeline);if(job.state==='enrolled')root.append(el('p','历史接入任务：此状态代表本地检查成功，中心连接需另行核对。','warning'));$('#detail-dialog').showModal();}
$('#login-form').addEventListener('submit',async event=>{event.preventDefault();token=event.target.elements.token.value.trim();try{await refresh();sessionStorage.setItem('ventoy-token',token);$('#login-dialog').close();$('#login-error').textContent='';event.target.reset();}catch(error){$('#login-error').textContent=error.message;}});
$('#logout').addEventListener('click',()=>{token='';sessionStorage.removeItem('ventoy-token');location.reload();});$('#refresh').addEventListener('click',()=>refresh().catch(e=>notify(e.message)));
setInterval(()=>{if(!document.hidden&&token)refresh().catch(()=>{});},10000);
if(token)refresh().catch(e=>notify(e.message));else showLogin();

$('#close-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${closeId}/close`,{name:f.elements.name.value,stopped:f.elements.stopped.checked,reason:f.elements.reason.value});notify('任务已结单，旧授权已失效；重新安装请创建新任务');});});
