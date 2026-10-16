import {mkdir,readFile,writeFile} from 'node:fs/promises';
const root=new URL('../frontend/',import.meta.url),out=new URL('dist/',root);await mkdir(out,{recursive:true});
// 纯本地静态界面，无安装步骤或远端构建。
for(const name of ['index.html','app.js','style.css','format.mjs','icon.svg']){const bytes=await readFile(new URL(name,root));new TextDecoder('utf-8',{fatal:true}).decode(bytes);await writeFile(new URL(name,out),bytes);}
console.log('Harness Session Manager frontend assets ready');
