import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
const library:any={id:'shelf',updated_at:'2026-11-07T12:00:00Z',shelf_enabled:true,shelf_config:{module_types:[{key:'module',name:'客户模块'}],regions:[{key:'region',name:'客户区域'}],stages:[{key:'first',name:'客户前期'},{key:'last',name:'客户后期'}],parsers:[]},entity_schema:{kinds:[],fields:[]}}
// Small spec, task 34: aliases remain editable configuration and share the
// region key/name namespace. API mocks replace the HTTP boundary only.
test('区域别名可编辑保存并阻止与其他区域键名重复', async t => {
 const {root,writes}=await mount(t)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='区域')!)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='编辑')!)
 let aliases=descendants(root).find(n=>n.tag==='textarea')!
 assert.ok(aliases, '区域编辑应提供别名列表')
 aliases.props.onInput({target:{value:'  客户别称  \nAlternative region'}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.deepEqual(writes[0][2].shelf_config.regions[0],{key:'region',name:'客户区域',aliases:['客户别称','Alternative region']})
 descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']===zh.shelfSettings.newName)!.props.onInput({target:{value:'另一区域'}});await settle()
 descendants(root).find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await settle()
 await click(descendants(root).filter(n=>n.tag==='button'&&visibleText(n)==='编辑')[1]!)
 aliases=descendants(root).find(n=>n.tag==='textarea')!
 aliases.props.onInput({target:{value:' REGION '}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.equal(writes.length,1)
 assert.ok(descendants(root).some(n=>n.props.role==='alert'))
 aliases.props.onInput({target:{value:'不同别称'}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.deepEqual(writes[1][2].shelf_config.regions[1].aliases,['不同别称'])
})
// Small spec, task 30: markets are maintained independently from sale regions.
test('市场可新增并原样保存且区域不变', async t => {
 const {root,writes}=await mount(t)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='市场')!)
 descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']===zh.shelfSettings.newName)!.props.onInput({target:{value:' 新市场 '}});await settle()
 descendants(root).find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.equal(writes[0][2].shelf_config.markets[0].name,' 新市场 ')
 assert.deepEqual(writes[0][2].shelf_config.regions,library.shelf_config.regions)
})
// Small spec, task 20 / ADR 0023: saved settings use the library's revision;
// configured names and keys survive editing without customer constants.
async function mount(t:test.TestContext,nodes:any[]=[],settings:{library?:any;saveError?:string;locale?:string}={}){
 const writes:any[]=[]
 let savedLibrary=structuredClone(settings.library || library)
 const ui={...entryPresentationTestModules['@/components/ui'] as object,Tabs:defineComponent({props:['modelValue','items'],emits:['update:modelValue'],setup:(p,{slots,emit})=>()=>h('div',[h('nav',p.items.map((item:any)=>h('button',{onClick:()=>emit('update:modelValue',item.value)},item.label))),slots[p.modelValue]?.()])})}
 const component=loadComponent(new URL('./ShelfSettingsPage.vue',import.meta.url),{
 ...entryPresentationTestModules,'@/components/ui':ui,
 'vue-router':{onBeforeRouteLeave(){},onBeforeRouteUpdate(){}},
 '@/api/model': { listModels: async () => [] },
 '@/api/library':{getCatalogLibrary:async()=>({success:true,data:savedLibrary})},
 '@/api/shelf-files':{shelfEntities:async()=>nodes,shelfWriteError:(cause:unknown,f:string)=>[f,(cause as Error).message].filter(Boolean).join(' · '),configureShelf:async(...args:any[])=>{writes.push(args);if(settings.saveError)throw new Error(settings.saveError);savedLibrary=JSON.parse(JSON.stringify({...savedLibrary,...args[2]}));return{success:true,data:{status:'confirmed'}}}},
 '@/components/ui/feedback':{confirm:()=>({result:Promise.resolve(true)})},
 './ShelfRequirementEditor.vue':{__esModule:true,default:defineComponent({setup:()=>()=>null})},
 './ShelfFieldEditor.vue':{__esModule:true,default:defineComponent({setup:()=>()=>null})},
 })
 const app=renderer.createApp(component,{library:savedLibrary});app.use(createI18n({legacy:false,locale:settings.locale || 'zh-CN',messages:{'zh-CN':zh,'en-US':en}}));const root=element('root');app.mount(root);t.after(()=>app.unmount());await settle();return{root,writes}
}

// Small spec, shelf-checklist-markets task 02: UI edits preserve stored keys;
// API mocks replace the HTTP boundary only.
test('区域对应市场读出已有配置并保存选择和不对应',async t=>{
 const savedLibrary=structuredClone(library)
 savedLibrary.shelf_config.regions.push({key:'region_b',name:'另一区域'})
 savedLibrary.shelf_config.markets=[{key:'domestic',name:'国内'},{key:'overseas',name:'海外'}]
 savedLibrary.shelf_config.checklist_markets_by_region={region:'domestic'}
 const {root,writes}=await mount(t,[],{library:savedLibrary})
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='区域')!)
 assert.match(visibleText(root),/区域对应市场/)
 const select=()=>descendants(root).find(n=>n.tag==='select'&&n.props.id==='checklist-market-region')!
 const other=()=>descendants(root).find(n=>n.tag==='select'&&n.props.id==='checklist-market-region_b')!
 assert.ok(select(),'每个区域应有一个市场下拉框')
 assert.equal(select().props.value,'domestic')
 assert.equal(other().props.value,'')
 select().props.onChange({target:{value:'overseas'}});await settle()
 other().props.onChange({target:{value:'domestic'}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.deepEqual(writes[0][2].shelf_config.checklist_markets_by_region,{region:'overseas',region_b:'domestic'})
 select().props.onChange({target:{value:''}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.deepEqual(writes[1][2].shelf_config.checklist_markets_by_region,{region_b:'domestic'})
})

// Small spec, task 02: show the API validation reason and retain failed edits.
test('删除被对应表引用的市场时展示校验错误并保留编辑',async t=>{
 const savedLibrary=structuredClone(library)
 savedLibrary.shelf_config.markets=[{key:'domestic',name:'国内'}]
 savedLibrary.shelf_config.checklist_markets_by_region={region:'domestic'}
 const message='market key domestic is referenced by checklist_markets_by_region for region region'
 const {root,writes}=await mount(t,[],{library:savedLibrary,saveError:message})
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='市场')!)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='移除')!)
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!)
 assert.match(visibleText(descendants(root).find(n=>n.props.role==='alert')!),/checklist_markets_by_region/)
 assert.deepEqual(writes[0][2].shelf_config.checklist_markets_by_region,{region:'domestic'})
 assert.deepEqual(writes[0][2].shelf_config.markets,[])
 assert.equal(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='确认保存')!.props.disabled,false)
})

// Small spec, task 02: empty markets offer only the localized unmapped choice.
test('没有市场时中英文每个区域只有不对应选项并保留长区域名',async t=>{
 for(const [locale,label] of [['zh-CN','不对应'],['en-US','No market']]){
  const savedLibrary=structuredClone(library)
  savedLibrary.shelf_config.regions[0].name='A very long configured region name '.repeat(3)
  const {root}=await mount(t,[],{library:savedLibrary,locale})
  const regionsLabel=locale==='en-US'?'Regions':'区域'
  await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)===regionsLabel)!)
  const select=descendants(root).find(n=>n.tag==='select'&&n.props.id==='checklist-market-region')!
  assert.ok(select)
  assert.deepEqual(descendants(select).filter(n=>n.tag==='option').map(n=>({value:n.props.value,text:visibleText(n)})),[{value:'',text:label}])
  assert.equal(descendants(root).find(n=>n.tag==='label'&&n.props.for==='checklist-market-region')?.text,savedLibrary.shelf_config.regions[0].name)
 }
})
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

// Small spec, task 22: the source column and list filter use list ownership,
// independently of an optional source label shared by both lists.
test('设置页来源清单显示所属清单并分别筛选同名要求',async t=>{
 const {root}=await mount(t,['01','02'].map(id=>({id,kind:'document_requirement',canonical_name:'参数表',attributes:{requirement:{list_name:id+' 清单',source_list:'来源原文',source_version:'V1'}}})))
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='资料要求')!)
 assert.match(visibleText(root),/01 清单/);assert.match(visibleText(root),/02 清单/)
 const filter=descendants(root).find(n=>n.tag==='select'&&n.props['aria-label']==='来源清单')!;filter.props.onChange({target:{value:'02 清单'}});await settle()
 const rows=descendants(root).filter(n=>n.tag==='tr'&&visibleText(n).includes('参数表'))
 assert.equal(rows.length,1);assert.match(visibleText(rows[0]!),/02 清单/);assert.doesNotMatch(visibleText(rows[0]!),/01 清单/)
})
