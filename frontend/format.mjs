export function bytes(value){if(value==null)return '未知';const n=Number(value);if(!Number.isFinite(n)||n<0)return '未知';if(!n)return '0 B';const i=Math.min(4,Math.floor(Math.log(n)/Math.log(1024)));return `${(n/1024**i).toLocaleString('zh-CN',{maximumFractionDigits:i?2:0})} ${['B','KiB','MiB','GiB','TiB'][i]}`;}
export function date(value){if(!value||String(value).startsWith('0001'))return '—';const d=new Date(value);return Number.isNaN(+d)?'—':d.toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'});}
export function filtered(items,query,sort='recent'){const q=query.trim().toLocaleLowerCase();const rows=items.filter(c=>!q||[c.title,c.cwd,c.id,c.relative].some(v=>String(v||'').toLocaleLowerCase().includes(q)));return rows.sort((a,b)=>sort==='size'?Number(b.bytes)-Number(a.bytes):new Date(b.modified)-new Date(a.modified));}
export function hideRequest(keys,preview){return {keys:[...keys],expectedKeys:preview.items.map(item=>item.key),source:preview.source,identity:preview.identity};}

// 工作区按原生登记顺序展示，搜索和排序只影响展示，不改变操作的完整工作区范围。
export function groupWorkspaces(catalog,query='',sort='recent'){
 const groups=(catalog.workspaces||[]).map(w=>({...w,allItems:[],items:[]}));const byKey=new Map(groups.map(w=>[w.key,w]));
 for(const c of catalog.items||[]){let group=byKey.get(c.workspaceKey);if(!group){const key=c.workspaceKey||'cwd:'+String(c.cwd||'').toLowerCase();group=byKey.get(key);if(!group){group={key,title:c.cwd?.split(/[\\/]/).filter(Boolean).at(-1)||'未记录工作区',path:c.cwd||'',registered:false,allItems:[],items:[]};groups.push(group);byKey.set(key,group);}}group.allItems.push(c);}
 const q=query.trim().toLocaleLowerCase();for(const group of groups){const matches=[group.title,group.path].some(v=>String(v||'').toLocaleLowerCase().includes(q));group.items=filtered(group.allItems,matches?'':q,sort);}
 return groups.filter(group=>!q||group.items.length||[group.title,group.path].some(v=>String(v||'').toLocaleLowerCase().includes(q)));
}
