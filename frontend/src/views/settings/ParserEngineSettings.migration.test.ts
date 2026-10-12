import assert from 'node:assert/strict'
import { readFileSync, appendFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createSSRApp, nextTick, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { changedSettings } from '../../utils/settingsPatch'
const require = createRequire(import.meta.url)
test('loaded parser credential refill preserves null, false and absent public fields in the actual request', async()=>{
 const calls:any[]=[]
 const loaded={mineru_api_key:'',mineru_cloud_model:'pipeline',mineru_enable_formula:null,mineru_cloud_enable_ocr:false}
 const modules:Record<string,any>={
  '@/utils/settingsPatch':{changedSettings},'vue-i18n':{useI18n:()=>({t:(key:string)=>key})},'tdesign-vue-next':{MessagePlugin:{}},
  '@/utils/request':{get:async(path:string)=>{calls.push({method:'GET',path});return {data:path.endsWith('/parser-engine-config')?loaded:[]}},put:async(path:string,body:any)=>{calls.push({method:'PUT',path,body});return {data:body}}}
 }
 const evaluate=(code:string)=>{const module={exports:{} as any};const js=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;new Function('require','module','exports',js)((name:string)=>name.endsWith('.vue')?{default:{}}:modules[name]??require(name),module,module.exports);return module.exports}
 modules['@/api/system']=evaluate(readFileSync(new URL('../../api/system.ts',import.meta.url),'utf8'))
 const {descriptor}=parse(readFileSync(new URL('./ParserEngineSettings.vue',import.meta.url),'utf8'))
 const component=evaluate(compileScript(descriptor,{id:'parser-refill'}).content).default
 component.render=()=>null
 const setup=component.setup
 component.setup=(props:any,context:any)=>{const state=setup(props,context);onServerPrefetch(async()=>{await state.loadAll();state.config.value.mineru_api_key='replacement';await state.onSave();await nextTick()});return state}
 const errors:any[]=[];const app=createSSRApp(component);app.config.errorHandler=e=>errors.push(e);await renderToString(app);assert.deepEqual(errors,[])
 const save=calls.find(x=>x.method==='PUT');assert.deepEqual(save.body,{...loaded,mineru_api_key:'replacement'})
 if(process.env.RELIQUARY_RECOVERY_REQUESTS)appendFileSync(process.env.RELIQUARY_RECOVERY_REQUESTS,JSON.stringify({kind:'parser_credentials',...save})+'\n')
})
