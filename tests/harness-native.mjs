import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFile,writeFile,readdir,lstat,rm} from 'node:fs/promises';
import {join,resolve,relative,isAbsolute} from 'node:path';
import {Context} from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/cordis/lib/index.js';
import SessionStore from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-session/lib/index.js';
import Persistence from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-session-persistence-jsonl/lib/index.js';
import Query from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-session-query-sqlite/lib/index.js';
import {foldSessionTitle} from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-session-title/lib/index.js';
import {readImageFile} from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-attachment-local/lib/index.js';
import {workspaceDomainState,workspaceRecord} from '../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/dsh-workspace/lib/index.js';
// 仅接受本任务固定合成来源；直接使用 Harness 未修改的持久化与 SQLite 查询库。
const url=new URL(process.argv[2]);assert.equal(url.hostname,'127.0.0.1');const token=url.searchParams.get('preview');assert.ok(token);
async function api(name,...args){const r=await fetch(new URL('/api/'+name,url),{method:'POST',headers:{'Content-Type':'application/json','X-DSH-Preview':token},body:JSON.stringify(args)});const x=await r.json();if(!r.ok)throw Error(x.error);return x;}
const cfg=await api('Settings');assert.equal(cfg.demo,true);assert.match(cfg.source,/dsh-space-demo-[^\\/]+[\\/]source$/);
const ctx=new Context();new SessionStore(ctx);const persistence=new Persistence(ctx,{root:join(cfg.source,'sessions'),compression:'zstd'});const query=new Query(ctx,{path:join(cfg.storage,'native-query-validation.sqlite'),openAt:'first-search'});await new Promise(setImmediate);
async function observation(){return {listed:(await query.listSessions()).map(x=>x.header.id).sort(),persisted:(await persistence.list()).map(x=>x.header.id).sort(),query:(await query.searchSessions({query:'Harness',limit:100})).items.map(x=>x.header.id).sort()};}
try {
 const catalog=await api('Conversations');assert.equal(catalog.items.length,3);assert.ok(catalog.items.every(x=>!x.warning));const item=catalog.items.find(x=>x.id==='session-demo-0'),keys=[item.key];
 assert.equal(catalog.workspaces.length,2);assert.equal(catalog.workspaces[1].title,'默认工作区');assert.equal(catalog.items.find(x=>x.id==='session-demo-1').archived,true);
 const handle=await persistence.open(item.id,'read');let events;try{events=(await handle.read()).events;}finally{await handle.close();}
 assert.equal(foldSessionTitle(events).title,item.title);assert.ok(!item.title.startsWith('session-'));
 const imageRef=events[0].data.content.find(x=>x.type==='image').attachment;
 const imageRoot=join(cfg.source,'attachments','v1'),imageDigest=imageRef.attachmentId.slice('sha256:'.length),imagePath=join(imageRoot,'objects',imageDigest.slice(0,2),imageDigest);
 const nativeImage=await readImageFile(imageRoot,imageRef);const imageBytes=Buffer.from(nativeImage.data);assert.equal(createHash('sha256').update(imageBytes).digest('hex'),imageDigest);
 const page=await api('Read',item.key,0);assert.equal(page.items[0].images[0].attachmentId,imageRef.attachmentId);assert.equal(Buffer.from((await api('Image',item.key,imageRef.attachmentId)).split(',')[1],'base64').equals(imageBytes),true);
 const before=await observation();assert.equal(before.persisted.length,3);assert.deepEqual(before.listed,before.persisted);assert.deepEqual(before.query,before.persisted);
 const original=await readFile(item.log),originalSHA=createHash('sha256').update(original).digest('hex');
 const backup=await api('Backup',keys);assert.equal(backup.formatVersion,2);assert.equal(backup.attachments.length,1);const preview=await api('PreviewHide',keys);assert.equal(preview.items.length,1);
 const hidden=await api('HideDirect',keys);
 const afterHide=await observation();assert.equal(afterHide.persisted.length,2);assert.deepEqual(afterHide.listed,afterHide.persisted);assert.ok(!afterHide.persisted.includes(item.id));assert.deepEqual(afterHide.query,afterHide.persisted);
 assert.equal((await api('Conversations')).items.length,2);assert.equal((await api('Backups')).length,1);
 await api('RestoreHidden',hidden.id);const afterUnhide=await observation();assert.deepEqual(afterUnhide,before);assert.deepEqual(await readFile(item.log),original);
 // 模拟本次合成来源文件丢失，删除目标必须是明确固定来源内的一个会话目录。
 const target=resolve(item.path),sessions=resolve(cfg.source,'sessions'),part=relative(sessions,target);assert.ok(part&&!isAbsolute(part)&&!part.startsWith('..'));assert.equal(part.split(/[\\/]/).length,2);assert.equal((await lstat(target)).isSymbolicLink(),false);await rm(target,{recursive:true});
 assert.equal((await observation()).persisted.length,2);
 // 图片目标由原生摘要推导且核对固定合成附件根后才删除，验证 ZIP 实际恢复外置图片。
 assert.match(imageDigest,/^[a-f0-9]{64}$/);assert.ok(!relative(resolve(imageRoot),resolve(imagePath)).startsWith('..'));await rm(imagePath);
 await api('RestoreBackup',backup.id);const afterBackupRestore=await observation();assert.deepEqual(afterBackupRestore,before);assert.deepEqual(await readFile(item.log),original);
 assert.deepEqual(Buffer.from((await readImageFile(imageRoot,imageRef)).data),imageBytes);assert.equal((await api('Conversations')).items.find(x=>x.id===item.id).title,item.title);
 const deletion=await api('PreviewDelete',{kind:'conversations',keys});assert.equal(deletion.items.length,1);assert.equal(deletion.images,1);assert.equal((await observation()).persisted.length,3);
 const deleted=await api('DeleteConfirmed',deletion.token);assert.equal(deleted.conversations,1);assert.equal(deleted.images,1);const afterPermanentDelete=await observation();assert.equal(afterPermanentDelete.persisted.length,2);assert.deepEqual(afterPermanentDelete.query,afterPermanentDelete.persisted);
 await api('RestoreBackup',backup.id);assert.deepEqual(await observation(),before);assert.deepEqual(Buffer.from((await readImageFile(imageRoot,imageRef)).data),imageBytes);
 const workspaceDelete=await api('PreviewDelete',{kind:'workspace',workspaceKey:catalog.workspaces[1].key});assert.equal(workspaceDelete.items.length,1);assert.equal(workspaceDelete.workspaceTitle,'默认工作区');await api('DeleteConfirmed',workspaceDelete.token);
 const afterWorkspaceDelete=await observation();assert.equal(afterWorkspaceDelete.persisted.length,2);assert.deepEqual(afterWorkspaceDelete.listed,afterWorkspaceDelete.persisted);assert.deepEqual(afterWorkspaceDelete.query,afterWorkspaceDelete.persisted);assert.equal((await api('Conversations')).workspaces.length,1);
 const registry=JSON.parse(await readFile(join(cfg.source,'storages','workspace.json'),'utf8'));workspaceDomainState.parse(registry.global);for(const record of Object.values(registry.tables.workspaces))workspaceRecord.parse(record);assert.equal(Object.keys(registry.tables.workspaces).length,1);
 await query.close();
 const hiddenRows=await api('Hidden');assert.equal(hiddenRows.length,0);assert.equal((await api('Backups')).length,1);
 await api('StartScan');let scan;for(let n=0;n<1000;n++){scan=await api('ScanState');if(!scan.running)break;await new Promise(r=>setTimeout(r,10));}assert.ok(scan.complete);assert.equal(scan.errors,0);
 let bytes=0,files=0;async function walk(dir){for(const entry of await readdir(dir,{withFileTypes:true})){const p=join(dir,entry.name),s=await lstat(p);assert.equal(s.isSymbolicLink(),false);if(s.isDirectory())await walk(p);else if(s.isFile()){bytes+=s.size;files++;}}}await walk(cfg.source);await walk(cfg.storage);assert.equal(scan.bytes,String(bytes));assert.equal(scan.files,files);assert.equal(scan.categories.reduce((n,c)=>n+BigInt(c.bytes),0n),BigInt(scan.bytes));
 const modules={};for(const name of ['dsh-session-persistence-jsonl','dsh-session-query-sqlite']){const p=new URL('../.cache/harness-runtime/dsh/node_modules/@deepseek-ai/'+name+'/lib/index.js',import.meta.url);modules[name]=createHash('sha256').update(await readFile(p)).digest('hex');}
 const receipt={complete:true,scope:'Harness 0.2.0-rc.2 unmodified persistence + SQLite query + title fold + image reader + workspace schema over synthetic data; DSH Space application HTTP bindings; no native GUI',before,afterHide,afterUnhide,afterBackupRestore,afterPermanentDelete,afterWorkspaceDelete,originalSessionSHA256:originalSHA,imageSHA256:imageDigest,nativeImageRestoredAndVerified:true,nativeTitleMatches:true,nativeWorkspaceSchemaValid:true,permanentDeletionVerified:true,scanLogicalBytes:scan.bytes,scanFiles:scan.files,scanIndependentMetadataMatches:true,autoBackupObserved:false,hiddenContainerCleaned:true,manualBackupRetained:true,modules,realUserDataAccessed:false,sourceCredentialsRead:false,uploaded:false};
 await writeFile(new URL('../.cache/checks/native-compatibility.json',import.meta.url),JSON.stringify(receipt,null,2),'utf8');console.log(JSON.stringify({complete:true,before:3,afterHide:2,afterUnhide:3,afterBackupRestore:3,scanBytes:scan.bytes,scanFiles:scan.files,scope:receipt.scope}));
} finally {await query.close();}
