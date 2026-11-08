import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { descendants, element, loadComponent, renderer, settle } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
// Small spec, task 20 / ADR 0024. Requirement editing preserves other attributes
// and saves explicitly chosen applicability in the same atomic changeset.
test('资料要求修改与产品线适用范围在同一变更集中确认',async t=>{
 const writes:any[]=[]
 const component=loadComponent(new URL('./ShelfRequirementEditor.vue',import.meta.url),{...entryPresentationTestModules,'@/api/shelf-files':{shelfWriteError:(_:unknown,f:string)=>f,writeShelf:async(_:string,ops:any)=>{writes.push(ops);return{success:true,data:{status:'confirmed'}}}},'@/api/relations':{listEntityRelations:async()=>({success:true,data:[]})},'@/components/ui/feedback':{confirm:()=>({result:Promise.resolve(true)})}})
 const app=renderer.createApp(component,{library:{id:'shelf',shelf_config:{stages:[{key:'last',name:'客户终期'}],regions:[],module_types:[]}},entity:{id:'req',canonical_name:'原要求',revision:7,attributes:{custom:'保留',requirement:{final_due_stage:'last',source_list:'客户清单'}}},nodes:[{id:'line',kind:'product_line',canonical_name:'客户产品线'}]});app.use(createI18n({legacy:false,locale:'zh-CN',messages:{'zh-CN':zh,'en-US':en}}));const root=element('root');app.mount(root);t.after(()=>app.unmount());await settle()
 descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']==='名称')!.props.onInput({target:{value:' 新要求原名 '}})
 const checkbox=descendants(root).find(n=>n.tag==='input'&&n.props.type==='checkbox')!;checkbox.props.onChange({target:{checked:true}});await settle()
 descendants(root).find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await settle()
 assert.equal(writes[0][0].type,'update_entity');assert.equal(writes[0][0].patch.attributes.custom,'保留');assert.equal(writes[0][0].patch.name,' 新要求原名 ')
 assert.equal(writes[0][1].type,'save_relation');assert.equal(writes[0][1].relation.object_id,'line');assert.equal(writes[0][1].relation.subject_id,'req')
})
