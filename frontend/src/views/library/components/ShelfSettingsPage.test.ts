import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
const library:any={id:'shelf',updated_at:'2026-11-07T12:00:00Z',shelf_enabled:true,shelf_config:{module_types:[{key:'module',name:'客户模块'}],regions:[{key:'region',name:'客户区域'}],stages:[{key:'first',name:'客户前期'},{key:'last',name:'客户后期'}],parsers:[]},entity_schema:{kinds:[],fields:[]}}
// Small spec, task 20 / ADR 0023: saved settings use the library's revision;
// configured names and keys survive editing without customer constants.
async function mount(t:test.TestContext){
 const writes:any[]=[]
 const ui={...entryPresentationTestModules['@/components/ui'] as object,Tabs:defineComponent({props:['modelValue','items'],emits:['update:modelValue'],setup:(p,{slots,emit})=>()=>h('div',[h('nav',p.items.map((item:any)=>h('button',{onClick:()=>emit('update:modelValue',item.value)},item.label))),slots[p.modelValue]?.()])})}
 const component=loadComponent(new URL('./ShelfSettingsPage.vue',import.meta.url),{
 ...entryPresentationTestModules,'@/components/ui':ui,
 'vue-router':{onBeforeRouteLeave(){},onBeforeRouteUpdate(){}},
 '@/api/model': { listModels: async () => [] },
 '@/api/library':{getCatalogLibrary:async()=>({success:true,data:library})},
 '@/api/shelf-files':{shelfEntities:async()=>[],shelfWriteError:(_:unknown,f:string)=>f,configureShelf:async(...args:any[])=>{writes.push(args);return{success:true,data:{status:'confirmed'}}}},
 '@/components/ui/feedback':{confirm:()=>({result:Promise.resolve(true)})},
 './ShelfRequirementEditor.vue':{__esModule:true,default:defineComponent({setup:()=>()=>null})},
 './ShelfFieldEditor.vue':{__esModule:true,default:defineComponent({setup:()=>()=>null})},
 })
 const app=renderer.createApp(component,{library});app.use(createI18n({legacy:false,locale:'zh-CN',messages:{'zh-CN':zh,'en-US':en}}));const root=element('root');app.mount(root);t.after(()=>app.unmount());await settle();return{root,writes}
}
test('设置页改名原样保存，并携带库更新时间确认变更集',async t=>{
 const {root,writes}=await mount(t)
 assert.match(visibleText(root),/客户模块/)
 assert.equal(descendants(root).filter(n=>n.tag==='input'&&n.props['aria-label']==='名称').length,0)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='编辑')!)
 const name=descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']==='名称')!;name.props.onInput({target:{value:' 客户新模块 '}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!);await settle()
 assert.equal(writes[0][0],'shelf');assert.equal(writes[0][1],library.updated_at);assert.deepEqual(writes[0][2].shelf_config.module_types,[{key:'module',name:' 客户新模块 '}])
 assert.deepEqual(writes[0][2].shelf_config.regions,library.shelf_config.regions)
})
test('阶段可重排，状态保持固定五种',async t=>{
 const {root,writes}=await mount(t)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='状态与阶段')!)
 assert.match(visibleText(root),/GA/);assert.match(visibleText(root),/Planning/)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='下移')!)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!);await settle()
 assert.deepEqual(writes[0][2].shelf_config.stages.map((s:any)=>s.key),['last','first'])
})

test('HTTP 解析器和按扩展名规则随设置保存',async t=>{
 const {root,writes}=await mount(t)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='解析器')!)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='新增解析器')!)
 const name=descendants(root).find(n=>n.tag==='input'&&String(n.props.id).endsWith('-name'))!;name.props.onInput({target:{value:' 客户解析服务 '}});await settle()
 const type=descendants(root).find(n=>n.tag==='select'&&String(n.props.id).endsWith('-type'))!;type.props.onChange({target:{value:'http'}});await settle()
 const url=descendants(root).find(n=>n.tag==='input'&&String(n.props.id).endsWith('-url'))!;url.props.onInput({target:{value:'http://localhost:7000/parse'}});await settle()
 const parserSelect=descendants(root).find(n=>n.tag==='select'&&n.props.id==='shelf-default-parser')!;const parserKey=descendants(parserSelect).find(n=>n.tag==='option'&&visibleText(n)===' 客户解析服务 ')!.props.value
 parserSelect.props.onChange({target:{value:parserKey}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='新增规则')!)
 descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']==='扩展名')!.props.onInput({target:{value:'.pdf'}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!);await settle()
 const config=writes[0][2].shelf_config;assert.equal(config.default_parser,config.parsers[0].key);assert.equal(config.parsers[0].name,' 客户解析服务 ');assert.equal(config.parsers[0].url,'http://localhost:7000/parse');assert.equal(config.parsers[0].timeout_seconds,120);assert.equal(config.parser_rules[0].extension,'.pdf');assert.equal(config.parser_rules[0].parser_key,config.default_parser)
})
