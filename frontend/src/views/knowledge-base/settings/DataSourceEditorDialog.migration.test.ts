import assert from 'node:assert/strict'
import { readFileSync, appendFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch, nextTick } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
const require = createRequire(import.meta.url)
const stub = defineComponent({setup(_, { slots }) {return () => h('div',slots.default?.())}})
const record = { id: 'restored', name: 'Private RSS', type: 'rss', status: 'paused', maintenance_required: true, maintenance_missing_fields: ['config.credentials.auth_headers'], credentials: { credentials: { configured: false } }, sync_schedule: '', sync_mode: 'incremental', sync_deletions: false, default_content_type: null, config: { resource_ids: null, settings: { feed_urls: 'https://example.test/feed', publisher: null, article_path_prefix: null, extra: { empty: [], missing: null } }, extension: null } }
for (const locale of ['zh-CN','en-US'] as const) for (const edit of ['credentials','name','publisher'] as const) {
 test(`${locale}: loaded restored RSS saves only changed ${edit} fields`, async () => {
  const calls: any[] = []
  const i18n = createI18n({legacy:false,locale,messages:{'zh-CN':zh,'en-US':en}})
  const modules: Record<string,any> = { 'vue-i18n':{useI18n:()=>i18n.global}, 'tdesign-vue-next':{MessagePlugin:{success(){},warning(){}}}, '@/types/entryClassification':{CONTENT_TYPES:[]}, '../utils/request':{
   get: async (url:string)=>{calls.push(['GET',url]);return {data:[record]}},
   put: async (url:string,payload:any)=>{calls.push(['PUT',url,payload]);return {}}, post:async()=>({}),del:async()=>({})
  }}
  const evaluate=(code:string)=>{const module={exports:{} as any};const compiled=ts.transpileModule(code,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022,esModuleInterop:true}}).outputText;new Function('require','module','exports',compiled)((name:string)=>name.endsWith('.vue')?{__esModule:true,default:stub}:modules[name]??require(name),module,module.exports);return module.exports}
  modules['@/utils/settingsPatch']=evaluate(readFileSync(new URL('../../../utils/settingsPatch.ts',import.meta.url),'utf8'))
  modules['@/api/datasource']=evaluate(readFileSync(new URL('../../../api/datasource.ts',import.meta.url),'utf8'))
  const response=await modules['@/api/datasource'].listDataSources()
  const source=readFileSync(new URL('./DataSourceEditorDialog.vue',import.meta.url),'utf8');const {descriptor}=parse(source);const script=compileScript(descriptor,{id:'rss-migration-test'});const template=compileTemplate({id:'rss-migration-test',filename:'DataSourceEditorDialog.vue',source:descriptor.template!.content,ssr:true,ssrCssVars:[],compilerOptions:{bindingMetadata:script.bindings}});assert.deepEqual(template.errors,[])
  const component=evaluate(script.content).default;component.ssrRender=evaluate(template.code).ssrRender;const setup=component.setup
  component.setup=(props:any,context:any)=>{const state=setup(props,context);onServerPrefetch(async()=>{
   state.visible.value=true;await nextTick();assert.equal(state.configured.value,false);assert.equal(state.headersRequired.value,true)
   await state.save();assert.equal(calls.filter(x=>x[0]==='PUT').length,0)
   state.form.value.authHeaders='Authorization: Bearer replacement'
   if(edit==='name')state.form.value.name='Renamed feed'
   if(edit==='publisher')state.form.value.publisher='New publisher'
   await state.save()
   assert.equal(calls.filter(x=>x[0]==='PUT').length,edit==='credentials'?1:2)
   if(edit==='name')assert.deepEqual(calls[2][2],{name:'Renamed feed'})
   if(edit==='publisher')assert.deepEqual(calls[2][2],{config:{settings:{publisher:'New publisher'}}})
   if(process.env.RELIQUARY_RECOVERY_REQUESTS && edit==='credentials')appendFileSync(process.env.RELIQUARY_RECOVERY_REQUESTS,JSON.stringify({kind:'rss_credentials',method:'PUT',body:calls[1][2],ordinary_config_requests:0})+'\n')

  });return state}
  const app=createSSRApp(component,{dataSource:response.data[0], visible:true});app.use(i18n)
  for(const name of ['t-form','t-form-item','t-input','t-select','t-button','t-radio','t-radio-group','t-switch','t-popconfirm'])app.component(name,stub)
  app.component('t-alert',defineComponent({props:['message'],setup:props=>()=>h('div',props.message)}))
  const failures:any[]=[];app.config.errorHandler=e=>failures.push(e);app.config.warnHandler=()=>{}
  const html=await renderToString(app);assert.equal(failures.length,0)
  assert.equal(calls[0][0],'GET');assert.equal(calls[1][1],'/api/v1/data-sources/restored/credentials');assert.deepEqual(calls[1][2],{credentials:{auth_headers:'Authorization: Bearer replacement'}});assert.equal(calls.length,edit==='credentials'?2:3)
  assert.ok(html.includes((locale==='zh-CN'?zh:en).datasource.field.migrationCredentialsRequired))
  modules['@/utils/cronHumanize']=evaluate(readFileSync(new URL('../../../utils/cronHumanize.ts',import.meta.url),'utf8'))
  const cardDescriptor=parse(readFileSync(new URL('./DataSourceSettings.vue',import.meta.url),'utf8')).descriptor
  const cardScript=compileScript(cardDescriptor,{id:'rss-card-test'});const cardTemplate=compileTemplate({id:'rss-card-test',filename:'DataSourceSettings.vue',source:cardDescriptor.template!.content,ssr:true,ssrCssVars:[],compilerOptions:{bindingMetadata:cardScript.bindings}})
  const card=evaluate(cardScript.content).default;card.ssrRender=evaluate(cardTemplate.code).ssrRender;const cardSetup=card.setup
  card.setup=(props:any,context:any)=>{const state=cardSetup(props,context);onServerPrefetch(()=>state.loadList());return state}
  const cardApp=createSSRApp(card);cardApp.use(i18n);cardApp.config.warnHandler=()=>{};cardApp.config.errorHandler=e=>failures.push(e)
  for(const name of ['t-loading','t-button','t-dropdown','t-dropdown-menu','t-dropdown-item','t-icon','t-popconfirm','t-tooltip','t-empty'])cardApp.component(name,stub)
  const cardHTML=await renderToString(cardApp);assert.deepEqual(failures,[]);assert.ok(cardHTML.includes((locale==='zh-CN'?zh:en).datasource.field.migrationCredentialsRequired));assert.equal(calls.at(-1)[0],'GET')

 })
}
