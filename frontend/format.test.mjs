import test from 'node:test';import assert from 'node:assert/strict';import {bytes,filtered,hideRequest,groupWorkspaces} from './format.mjs';
test('容量分级与未知值',()=>{assert.equal(bytes(null),'未知');assert.equal(bytes('0'),'0 B');assert.equal(bytes('1073741824'),'1 GiB');assert.match(bytes('82388827364'),/GiB$/);assert.equal(bytes(-1),'未知');});
test('完整目录可搜索且不修改来源顺序',()=>{const a={key:'a',title:'甲',cwd:'D:\\完整项目\\子目录',bytes:'2',modified:'2026-01-01'},b={key:'b',title:'乙',bytes:'12',modified:'2026-02-01'};const list=[a,b];assert.deepEqual(filtered(list,'完整项目'),[a]);assert.deepEqual(filtered(list,'','size'),[b,a]);assert.deepEqual(list,[a,b]);});
test('隐藏提交包含预览的精确会话组与来源身份',()=>{const p={items:[{key:'parent'},{key:'child'}],source:'fixed-source',identity:'native-id'};assert.deepEqual(hideRequest(['child'],p),{keys:['child'],expectedKeys:['parent','child'],source:'fixed-source',identity:'native-id'});});
test('工作区保持登记顺序、空工作区和完整路径，搜索不缩小操作范围',()=>{
 const catalog={workspaces:[{key:'b',title:'默认工作区',path:'D:\\完整文件夹\\default-workspace'},{key:'a',title:'自定义名称',path:'C:\\project'},{key:'empty',title:'空工作区',path:'C:\\empty'}],items:[{key:'1',workspaceKey:'a',title:'备注一',cwd:'C:\\project',modified:'2026-01-01'},{key:'2',workspaceKey:'a',title:'备注二',cwd:'C:\\project',modified:'2026-01-02'}]};
 const groups=groupWorkspaces(catalog);assert.deepEqual(groups.map(g=>g.key),['b','a','empty']);assert.equal(groups[0].path,catalog.workspaces[0].path);assert.equal(groups[2].allItems.length,0);
 const search=groupWorkspaces(catalog,'备注一');assert.equal(search.length,1);assert.equal(search[0].items.length,1);assert.equal(search[0].allItems.length,2);assert.equal(groupWorkspaces(catalog,'自定义名称')[0].items.length,2);
});
