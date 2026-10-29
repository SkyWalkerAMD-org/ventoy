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
const osNames = {centos:'CentOS',rocky:'Rocky Linux',almalinux:'AlmaLinux',rhel:'Red Hat Enterprise Linux',ubuntu:'Ubuntu Server',debian:'Debian',windows:'Windows'};
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
function route() { const page = location.hash.slice(1) || 'overview'; const chosen = ['overview','machines','jobs','images','software','profiles','pxe'].includes(page) ? page : 'overview'; $$('.page').forEach(p => {p.hidden = p.id !== chosen;}); $$('[data-nav]').forEach(a => a.classList.toggle('active', a.dataset.nav === chosen)); $('#breadcrumb').textContent = {overview:'部署总览',machines:'目标机器',jobs:'安装任务',images:'镜像管理',software:'软件分发',profiles:'系统版本',pxe:'PXE 自动安装'}[chosen]; if(chosen==='software' && token) refreshSoftware(); }
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
    if(job.state==='ready')actions.append(button('下载安装包',()=>downloadBundle(job)));
    if(job.os!=='windows' && (job.state==='ready'||job.preparation))actions.append(button('iVentoy 文件',()=>openPreparation(job),'primary'));
    actions.append(button('记录',()=>details(job)));
    if(['installing','enrolling'].includes(job.state))actions.append(button('结束失败任务',()=>{closeId=job.id;$('#close-form').reset();$('#close-target').textContent='目标机器：'+job.machine.name;$('#close-dialog').showModal();}));
    if(['draft','ready'].includes(job.state))actions.append(button('取消',async()=>{await post(`/api/jobs/${job.id}/cancel`);await refresh();}));
    return [name,system,el('span',states[job.state]||job.state,`badge ${job.state}`),stamp(job.created),actions];
  }));
}
function renderAll() {
  $('#version').textContent=data.version;$('#count-machines').textContent=data.machines.length;$('#count-ready').textContent=data.jobs.filter(j=>j.state==='ready').length;$('#count-running').textContent=data.jobs.filter(j=>['installing','enrolling'].includes(j.state)).length;$('#count-done').textContent=data.jobs.filter(j=>['installed','enrolled'].includes(j.state)).length;
  renderJobs($('#recent-jobs'),data.jobs.slice(0,6));renderJobs($('#all-jobs'),data.jobs);
  renderMachines(); renderBoot(); renderImages(); renderProfiles(); renderPXE();
  if(location.hash==='#software')refreshSoftware();
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
  for(const os of ['rocky','centos','almalinux','rhel','ubuntu','debian','windows']) {
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
function updateLinuxPasswordFields(f,win=false){const shared=!!data.linux_initial_password_configured;const input=f.elements.password_hash;input.required=!win&&!shared;input.disabled=win||shared;input.closest('label').hidden=win||shared;const username=f.elements.username;username.readOnly=!win&&!!data.linux_admin_username;if(username.readOnly)username.value=data.linux_admin_username;const note=$('[data-password-policy]',f);note.hidden=win||(!shared&&!username.readOnly);note.textContent=(username.readOnly?`后续安装的管理员账户统一为 ${data.linux_admin_username}。`:'')+(shared?'管理员和 root 使用服务端配置的统一初始密码。':'');}
function updateJobFields(){const f=$('#job-form');const profile=profileFor(f.elements.profile.value);const win=profile?.os==='windows';$$('[data-windows]',f).forEach(n=>n.hidden=!win);$$('[data-linux]',f).forEach(n=>n.hidden=win);f.elements.password.required=win;updateLinuxPasswordFields(f,win);$('#profile-hint').textContent=profile?`${profile.media}。${profile.notes}`:'请选择目标系统版本。';}
$('#job-form').elements.profile.addEventListener('change',updateJobFields);
$('#job-form').elements.iso.addEventListener('change',()=>{const f=$('#job-form');const image=data.images.find(i=>i.path===f.elements.iso.value);if(image?.suggested_profile)f.elements.profile.value=image.suggested_profile;updateJobFields();});
$$('[data-action]').forEach(b=>b.addEventListener('click',()=>b.dataset.action==='new-machine'?newMachine():b.dataset.action==='new-pxe'?newPXE():newJob()));
async function submit(form,action){const b=$('button[type=submit]',form)||$('button.primary:last-child',form)||$('button.danger',form);if(b)b.disabled=true;try{await action();await refresh(true);form.closest('dialog').close();}catch(error){notify(error.message);}finally{if(b)b.disabled=false;}}
async function openPreparation(job) {
  const plan=await (await api(`/api/jobs/${job.id}/preparation`)).json();
  const root=$('#preparation-body');root.replaceChildren();
  root.append(el('p',plan.prepared?'任务文件已准备。请在 iVentoy 中完成以下关联。':'在同机 iVentoy 目录生成安装脚本和独立 ISO 硬链接。'));
  const dl=el('dl',null,'detail-grid');
  for(const [label,value] of [['目标机器',job.machine.name],['启动 MAC',plan.mac],['镜像路径',plan.image_selection||'—'],['自动安装脚本',plan.script_selection||'—']])dl.append(el('dt',label),el('dd',value));
  if(plan.injection_selection)dl.append(el('dt','启动注入文件'),el('dd',plan.injection_selection));
  root.append(dl);
  if(plan.reason)root.append(el('p',plan.reason,'warning'));
  const steps=el('ol',null,'preparation-steps');
  if(plan.injection_selection)steps.append(el('li','为上述任务镜像同时设置“启动注入文件”，选择显示的启动兼容包；自动安装脚本仍需单独关联。'));
  for(const text of ['准备文件后，在 iVentoy 镜像管理刷新并选择上述镜像，关联自动安装脚本。','将脚本设为默认，设置非零选择超时；在专业版分机独启中把上述 MAC 绑定到此镜像。','客户端从网卡启动。任务结束后，先移除 iVentoy 中的关联，再清理任务文件。'])steps.append(el('li',text));
  root.append(steps);
  const actions=el('div',null,'dialog-actions');
  if(plan.can_prepare)actions.append(button(plan.prepared?'核对已准备文件':'准备安装文件',async()=>{await post(`/api/jobs/${job.id}/prepare`);await refresh(true);await openPreparation(job);notify('安装文件已准备；请按页面指引完成 iVentoy 关联');},'primary'));
  if(plan.can_cleanup)actions.append(button('清理任务文件',async()=>{await post(`/api/jobs/${job.id}/cleanup`);await refresh(true);$('#preparation-dialog').close();notify('任务文件已清理，原始 ISO 保留');}));
  root.append(actions);
  if(!$('#preparation-dialog').open)$('#preparation-dialog').showModal();
}
$('#machine-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const body=Object.fromEntries(new FormData(f));body.windows_disk=Number(body.windows_disk);await post('/api/machines',body);notify('目标机器已登记');});});
$('#job-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{const form=new FormData(f);const profile=profileFor(form.get('profile'));if(!profile)throw new Error('请选择系统版本');const win=profile.os==='windows';const body={machine_id:form.get('machine_id'),profile:profile.id,os:profile.os,iso:form.get('iso'),username:form.get('username'),password:win?form.get('password'):'',password_hash:win?'':form.get('password_hash'),ssh_key:win?'':form.get('ssh_key'),filesystem:win?'':form.get('filesystem'),windows_image:Number(form.get('windows_image')),locale:form.get('locale'),enroll:false};await post('/api/jobs',body);notify('草稿已保存；授权后可生成安装包');});});
function openArm(job){armId=job.id;$('#arm-form').reset();$('#arm-target').textContent=`${job.machine.name} · ${job.machine.mac} · 磁盘 ${job.machine.disk_serial}`;$('#arm-dialog').showModal();}
$('#arm-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${armId}/arm`,{name:f.elements.name.value,disk_serial:f.elements.disk_serial.value,erase:f.elements.erase.checked});notify('已授权；下载并配置安装包后，从目标机器网卡启动');});});
async function downloadBundle(job){const response=await api(`/api/jobs/${job.id}/bundle`);const url=URL.createObjectURL(await response.blob());const a=el('a');a.href=url;a.download=`ventoy-${job.machine.name}-${job.id}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('安装包已下载，请按包内 README 配置 iVentoy');}
$('#download-linux-policy').addEventListener('click',async event=>{const b=event.currentTarget;b.disabled=true;try{const response=await api('/api/linux-update-policy');const url=URL.createObjectURL(await response.blob());const a=el('a');a.href=url;a.download='linux-update-policy.sh';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('脚本已下载，请在手动安装完成的新系统中执行');}catch(error){notify(error.message);}finally{b.disabled=false;}});
$('#download-linux-manual').addEventListener('click',async event=>{const b=event.currentTarget;b.disabled=true;try{const response=await api('/api/linux-manual-setup?format=run');const url=URL.createObjectURL(await response.blob());const a=el('a');a.href=url;a.download='ventoy-linux-setup.run';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('完整配置包已下载；赋予执行权限后运行 sudo ./ventoy-linux-setup.run，即完成完整配置和软件安装');}catch(error){notify(error.message);}finally{b.disabled=false;}});
$('#download-software-setup').addEventListener('click',()=>$('#download-linux-manual').click());
$('#software-refresh').addEventListener('click',refreshSoftware);
async function refreshSoftware(){
  try{
    const s=await(await api('/api/software')).json();
    $('#software-directory').textContent=s.directory||'尚未配置 software_dir';
    $('#software-warnings').textContent=(s.warnings||[]).join('；');
    const rows=(s.flat_files||[]).map(f=>['自动匹配',f.path,(f.size/1048576).toFixed(2)+' MiB']);
    rows.push(...(s.archive_files||[]).map(f=>['仅传输到 /root/ventoy-tools',f.path,(f.size/1048576).toFixed(2)+' MiB']));
    rows.push(...(s.copy_files||[]).map(f=>['仅传输到 /root/ventoy-tools',f.path,(f.size/1048576).toFixed(2)+' MiB']));
    rows.push(...s.groups.map(g=>[g.id,g.files.map(f=>f.path.split('/').pop()).join('、'),g.errors?.length?g.errors.join('；'):'专属默认组']));
    if(!rows.length){empty($('#software-table'),s.enabled?'软件目录已就绪，等待上传':'尚未启用软件目录','放入 RPM / DEB 软件包、压缩包或独立工具后刷新即可，无需重启服务。');return;}
    table($('#software-table'),['处理方式 / 范围','文件','大小 / 状态'],rows);
  }catch(error){$('#software-warnings').textContent=error.message;}
}
function details(job){const root=$('#job-detail');const dl=el('dl',null,'detail-grid');const disks=job.storage_plan?.mode==='raid1-lvm'?'双盘 RAID1 + LVM：'+job.storage_plan.disk_serials.join('、'):job.machine.disk_serial;for(const [key,value] of [['任务编号',job.id],['节点',job.machine.name],['系统版本',jobSystem(job)],['目标磁盘',disks],...(job.os==='windows'?[]:[['数据文件系统',job.filesystem||'ext4']]),['状态',states[job.state]],['到期时间',job.state==='draft'?'尚未授权':stamp(job.expires)]]){dl.append(el('dt',key),el('dd',value));}const timeline=el('ol',null,'timeline');job.events.forEach(e=>{const li=el('li');li.append(el('strong',states[e.state]),el('small',` · ${stamp(e.time)}`));if(e.message)li.append(el('p',e.message));timeline.append(li);});root.replaceChildren(dl,timeline);if(job.state==='enrolled')root.append(el('p','历史接入任务：此状态代表本地检查成功，中心连接需另行核对。','warning'));$('#detail-dialog').showModal();}
$('#login-form').addEventListener('submit',async event=>{event.preventDefault();token=event.target.elements.token.value.trim();try{await refresh();sessionStorage.setItem('ventoy-token',token);$('#login-dialog').close();$('#login-error').textContent='';event.target.reset();}catch(error){$('#login-error').textContent=error.message;}});
$('#logout').addEventListener('click',()=>{token='';sessionStorage.removeItem('ventoy-token');location.reload();});$('#refresh').addEventListener('click',()=>refresh().catch(e=>notify(e.message)));
setInterval(()=>{if(!document.hidden&&token)refresh().catch(()=>{});},10000);
if(token)refresh().catch(e=>notify(e.message));else showLogin();

$('#close-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post(`/api/jobs/${closeId}/close`,{name:f.elements.name.value,stopped:f.elements.stopped.checked,reason:f.elements.reason.value});notify('任务已结单，旧授权已失效；重新安装请创建新任务');});});

function pxeSupported(p){return ['kickstart','preseed','autoinstall','ubiquity'].includes(p.installer);}
function newPXE(){
  const f=$('#pxe-form');f.reset();
  setOptions(f.elements.profile,data.profiles.filter(pxeSupported),p=>p.label,p=>p.id);
  setOptions(f.elements.iso,data.images||[],i=>i.path,i=>i.path);
  updateLinuxPasswordFields(f);
  $('#pxe-dialog').showModal();
}
$('#pxe-form').elements.iso.addEventListener('change',()=>{
  const f=$('#pxe-form'), item=data.images.find(i=>i.path===f.elements.iso.value);
  if(item){f.elements.label.value=item.path.split('/').at(-1).replace(/\.iso$/i,'')+' 自动安装';const p=profileFor(item.suggested_profile);if(p&&pxeSupported(p))f.elements.profile.value=p.id;}
});
$('#pxe-form').addEventListener('submit',event=>{event.preventDefault();const f=event.target;submit(f,async()=>{await post('/api/pxe/entries',Object.fromEntries(new FormData(f)));f.reset();notify('入口已保存，请准备并关联 iVentoy');});});
function renderPXE(){
  const entries=data.pxe_entries||[];
  if(!entries.length){empty($('#pxe-table'),'准备第一个系统入口','选择系统和自动安装脚本后，单内置盘直接安装；多盘在 PXE 画面选盘并输入 yes。',['添加系统入口',newPXE]);return;}
  table($('#pxe-table'),['系统入口','原始镜像','选盘方式','状态','操作'],entries.map(e=>{
    const actions=el('div',null,'actions');
    actions.append(button('准备与关联',()=>pxeDetails(e)));
    if(e.preparation?.prepared_at&&!e.preparation.prepared_at.startsWith('0001'))actions.append(button(e.enabled?'停用入口':'启用入口',async()=>{await post(`/api/pxe/entries/${e.id}/enabled`,{enabled:!e.enabled});await refresh(true);notify(e.enabled?'入口已停用，未开始的安装授权已撤销':'入口已启用；请确认 iVentoy 关联完成');}));
    const selection=e.disk_policy==='single-or-console'?'单盘自动安装；多盘在 PXE 选盘':e.disk_policy==='single-internal'?'旧策略：多盘需预先指定':'控制台选盘并确认';
    return[e.label,e.iso,selection,e.enabled?'已启用':'未启用',actions];
  }));
}
function pxeDetails(entry){
  const root=$('#preparation-body');root.replaceChildren(el('h3',entry.label));
  const p=entry.preparation;
  if(p){
    const absolute=name=>p.root.replace(/[\\/]$/,'')+'/'+name;
    const dl=el('dl',null,'detail-grid');
    for(const [label,value] of [['菜单镜像',p.iso_path.replace(/^iso\//,'')],['自动安装脚本',absolute(p.script_path)]])dl.append(el('dt',label),el('dd',value));
    const injection=Object.keys(p.files).find(n=>n.endsWith('.tar.gz'));
    if(injection)dl.append(el('dt','启动注入文件'),el('dd',absolute(p.script_path.replace(/[^/]+$/,injection))));
    root.append(dl);
  }
  const steps=el('ol',null,'preparation-steps');
  const local=entry.disk_policy==='single-or-console';
  const automatic=local||entry.disk_policy==='single-internal';
  for(const text of ['准备文件后，在 iVentoy 镜像管理选择上面的原始 ISO；同一入口提供手动与自动安装。','关联上面的自动安装脚本；若列有启动注入文件，也需要关联。','脚本默认编号设为 0、超时设为 0。Boot without auto install script 为手动安装；Boot with 对应脚本为自动安装。',automatic?'在本页启用入口。客户端选择系统与自动安装脚本后，只有一块可用内置盘时直接清空安装，无需选盘或输入 yes。':'在本页启用入口。客户端选择系统后，核对磁盘并输入 yes 确认安装；no 或 q 取消。'])steps.append(el('li',text));
  root.append(steps,el('p',local?'多盘、旧 LVM / RAID 或旧系统 swap 占用时，可在 PXE 画面选择目标盘并输入 yes 覆盖重装。旧存储须完整位于所选磁盘内，随后自动分区、安装和重启，无需登记机器或在后台创建任务。安装完成后从硬盘启动。':automatic?'此入口使用旧策略：多盘先指定目标盘并授权任务。迁移入口后可改为在 PXE 选盘。已有 LVM / RAID 仍需单独处理。':'已有匹配的待启动任务会使用原授权。再次网启已安装的机器需要重新确认。'));
  root.append(button('准备 / 核对菜单文件',async()=>{const updated=await (await post(`/api/pxe/entries/${entry.id}/prepare`)).json();await refresh(true);pxeDetails(updated);},'primary'));
  if(!$('#preparation-dialog').open)$('#preparation-dialog').showModal();
}
