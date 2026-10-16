import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import * as format from './format.mjs';

// 使用最小 DOM 替身验证危险操作的交互边界，不代替真实桌面布局验收。
class Element {
 constructor(tag){this.tagName=tag;this.children=[];this.attributes={};this.style={};this.className='';this.ownText='';}
 append(...children){for(const child of children){child.parent=this;this.children.push(child);}}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(value){this.ownText=String(value);this.children=[];}
 get textContent(){return this.ownText+this.children.map(x=>x.textContent).join('');}
 setAttribute(name,value){this.attributes[name]=value;}
 remove(){if(this.parent)this.parent.children=this.parent.children.filter(x=>x!==this);}
 querySelector(tag){return descendants(this).find(x=>x.tagName===tag)||null;}
 focus(){}
}
function descendants(node){return node.children.flatMap(child=>[child,...descendants(child)]);}
const source=await readFile(new URL('./app.js',import.meta.url),'utf8');
function harness(overrides={}){
 const roots={'#app':new Element('div'),'#overlay':new Element('div'),'#toast':new Element('div')};const calls=[];
 const catalog={warnings:[],workspaces:[{key:'workspace',title:'我的工作区',path:'D:\\完整目录\\我的工作区',registered:true}],items:[{key:'session',title:'对话备注',workspaceKey:'workspace',cwd:'D:\\完整目录\\我的工作区',id:'session-1',bytes:'100',modified:'2026-10-16'}]};
 const methods={Settings:async()=>({source:'C:\\合成来源',storage:'C:\\合成存储'}),Conversations:async()=>catalog,Hidden:async()=>[],Backups:async()=>[],ScanState:async()=>({}),HideDirect:async()=>({}),PreviewDelete:async()=>({token:'confirmed-scope-token',items:[{title:'对话备注',cwd:catalog.workspaces[0].path,hidden:false}],images:1,source:'C:\\合成来源'}),DeleteConfirmed:async()=>({conversations:1}),...overrides};
 const app=Object.fromEntries(Object.entries(methods).map(([name,fn])=>[name,async(...args)=>{calls.push({name,args});return fn(...args);}]))
 const context=vm.createContext({...format,URLSearchParams,Map,Set,document:{createElement:tag=>new Element(tag),querySelector:s=>roots[s],addEventListener(){},removeEventListener(){}},window:{go:{main:{App:app}},addEventListener(){}},location:{search:''},setTimeout:()=>1,clearTimeout(){}});
 const body=source.replace(/^import[^\n]+\n/,'').replace(/refresh\(\);\s*$/,'');
 vm.runInContext(body+'\nglobalThis.ui={state,render,hide,permanentDelete};',context);
 Object.assign(context.ui.state,{settings:{source:'C:\\合成来源',storage:'C:\\合成存储'},catalog});context.ui.render();
 return {...context.ui,roots,calls};
}
async function waitDialog(h){for(let n=0;n<30;n++){const dialog=descendants(h.roots['#overlay']).find(x=>x.attributes.role==='dialog'||x.attributes.role==='alertdialog');if(dialog)return dialog;await new Promise(setImmediate);}throw Error('弹窗未出现');}
function click(dialog,label){const button=descendants(dialog).find(x=>x.tagName==='button'&&x.textContent===label);assert.ok(button,'缺少按钮 '+label);assert.equal(button.disabled,false);button.onclick();}

test('隐藏直接执行，不经过确认或创建 ZIP',async()=>{
 const h=harness();await h.hide(['session']);assert.deepEqual(h.calls.filter(x=>x.name==='HideDirect').map(x=>x.args),[[['session']]]);assert.equal(h.calls.some(x=>x.name==='Backup'),false);assert.equal(h.roots['#overlay'].children.length,0);
});
test('Harness 未退出时弹窗展示完整错误，关闭后页面保留提示',async()=>{
 const message='请先退出 DeepSeek Harness（包括托盘、网页服务和命令行），再执行隐藏或恢复';const h=harness({HideDirect:async()=>{throw Error(message);}});const pending=h.hide(['session']);const dialog=await waitDialog(h);assert.equal(dialog.attributes.role,'alertdialog');assert.ok(dialog.textContent.includes(message));click(dialog,'知道了');await pending;assert.ok(h.roots['#app'].textContent.includes(message));
});
test('永久删除取消不提交，确认后才提交冻结的范围令牌',async()=>{
 const h=harness();let pending=h.permanentDelete({kind:'conversations',keys:['session']});let dialog=await waitDialog(h);assert.ok(dialog.textContent.includes('无法撤销'));assert.ok(dialog.textContent.includes('对话备注'));assert.equal(h.calls.some(x=>x.name==='DeleteConfirmed'),false);click(dialog,'取消');await pending;assert.equal(h.calls.some(x=>x.name==='DeleteConfirmed'),false);
 pending=h.permanentDelete({kind:'workspace',workspaceKey:'workspace'});dialog=await waitDialog(h);assert.equal(h.calls.some(x=>x.name==='DeleteConfirmed'),false);click(dialog,'永久删除');await pending;assert.deepEqual(h.calls.filter(x=>x.name==='DeleteConfirmed').map(x=>x.args),[['confirmed-scope-token']]);
});
test('分组列表直接显示工作区名称和完整路径',()=>{const h=harness();assert.ok(h.roots['#app'].textContent.includes('我的工作区'));assert.ok(h.roots['#app'].textContent.includes('D:\\完整目录\\我的工作区'));assert.ok(h.roots['#app'].textContent.includes('永久删除工作区'));});
