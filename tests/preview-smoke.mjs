import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {readFile} from 'node:fs/promises';
import {resolve} from 'node:path';

// 生产 EXE 固定使用自建的合成来源，验证发布包功能；不接受真实用户数据目录。
const config=JSON.parse(await readFile(new URL('../wails.json',import.meta.url),'utf8'));
const exe=resolve(process.argv[2]||`build/bin/HarnessSessionManager-${config.info.productVersion}.exe`);
const child=spawn(exe,['--preview'],{windowsHide:true,stdio:['ignore','pipe','pipe']});
const startup=new Promise((resolveReady,reject)=>{
 let output='';child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{output+=chunk;const match=output.match(/DSH_SPACE_PREVIEW (http:\/\/127\.0\.0\.1:\d+\/\?preview=[a-f0-9]+)\r?\n/);if(match)resolveReady(new URL(match[1]));});
 child.once('error',reject);child.once('exit',code=>reject(Error(`预览进程提前退出：${code}`)));
});
const timer=setTimeout(()=>child.kill(),90_000);
try{
 const url=await Promise.race([startup,new Promise((_,reject)=>setTimeout(()=>reject(Error('合成预览启动超时')),15_000).unref())]);
 async function api(method,...args){const response=await fetch(new URL('/api/'+method,url),{method:'POST',headers:{'Content-Type':'application/json','X-DSH-Preview':url.searchParams.get('preview')},body:JSON.stringify(args)});const result=await response.json();if(!response.ok)throw Error(result.error||'API 失败');return result;}
 const cfg=await api('Settings');assert.equal(cfg.demo,true);assert.match(cfg.source.replaceAll('\\','/'),/\/dsh-space-demo-[^/]+\/source$/);
 const catalog=await api('Conversations');assert.equal(catalog.items.length,3);assert.equal(catalog.workspaces.length,2);const conversation=catalog.items.find(c=>c.id==='session-demo-0');assert.ok(conversation&&!conversation.warning);assert.ok(!conversation.title.startsWith('session-'));
 const page=await api('Read',conversation.key,0);const image=page.items[0].images[0];assert.match(await api('Image',conversation.key,image.attachmentId),/^data:image\/png;base64,/);
 const backup=await api('Backup',[conversation.key]);assert.equal(backup.attachments.length,1);
 const hidden=await api('HideDirect',[conversation.key]);assert.equal((await api('Conversations')).items.length,2);assert.equal((await api('Backups')).length,1);
 await api('RestoreHidden',hidden.id);assert.equal((await api('Hidden')).length,0);assert.equal((await api('Conversations')).items.length,3);
 const preview=await api('PreviewDelete',{kind:'conversations',keys:[conversation.key]});assert.equal(preview.items.length,1);assert.equal((await api('Conversations')).items.length,3);
 const deleted=await api('DeleteConfirmed',preview.token);assert.equal(deleted.conversations,1);assert.equal(deleted.images,1);await api('RestoreBackup',backup.id);assert.equal((await api('Conversations')).items.length,3);assert.match(await api('Image',conversation.key,image.attachmentId),/^data:image\/png;base64,/);
 const workspace=await api('PreviewDelete',{kind:'workspace',workspaceKey:catalog.workspaces[1].key});assert.equal(workspace.items.length,1);await api('DeleteConfirmed',workspace.token);assert.equal((await api('Conversations')).workspaces.length,1);assert.equal((await api('Conversations')).items.length,2);
 console.log('生产 EXE 合成验收通过：工作区、备注、图片、手动备份、直接隐藏恢复、永久删除及 ZIP 恢复。');
}finally{clearTimeout(timer);child.kill();}
