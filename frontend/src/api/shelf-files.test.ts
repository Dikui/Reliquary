import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
// Small spec, task 20 / ADR 0023/0024: replace transport at the HTTP boundary.
function load() {
 const calls:any[]=[];const module={exports:{} as any};const code=ts.transpileModule(readFileSync(new URL('./shelf-files.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
 new Function('require','module','exports',code)((name:string)=>name==='@/utils/request'?Object.fromEntries(['get','post'].map(method=>[method,async(url:string,data:any)=>{calls.push({method,url,data});return{success:true,data:[]}}])):{listEntities:async(input:any)=>{calls.push(input);return{success:true,data:input.offset===0?Array.from({length:100},(_,id)=>({id})):[]}}},module,module.exports);return{api:module.exports,calls}
}
test('文件筛选按版本分页并编码搜索内容',async()=>{const{api,calls}=load();await api.listShelfFiles('shelf/a',{q:'原名&资料',include_history:true,offset:50});const url=new URL(calls[0].url,'https://example.invalid');assert.equal(url.pathname,'/api/v1/libraries/shelf%2Fa/shelf/documents');assert.equal(url.searchParams.get('q'),'原名&资料');assert.equal(url.searchParams.get('include_history'),'true');assert.equal(url.searchParams.get('offset'),'50')})
test('网页保存确认有类型变更集并带来源及每项决定',async()=>{const{api,calls}=load();await api.configureShelf('shelf','saved-time',{shelf_config:{regions:[{key:'r',name:' 原名 '}]}});assert.deepEqual(calls[0].data,{source:{type:'web'},confirmed:true,operations:[{id:'settings',type:'configure_shelf',library_updated_at:'saved-time',library:{shelf_config:{regions:[{key:'r',name:' 原名 '}]}}}],decisions:[{item_id:'settings',action:'apply'}]})})
test('节点选择读取全部分页避免隐藏可选身份',async()=>{const{api,calls}=load();const nodes=await api.shelfEntities('shelf','part');assert.equal(nodes.length,100);assert.equal(calls[1].offset,100);assert.equal(calls[1].kind,'part')})
