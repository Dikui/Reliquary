import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { descendants, element, loadComponent, renderer, settle } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
// Small spec, task 20 / ADR 0023: parameter fields stay scoped to their lines.
test('参数字段编辑保留产品线范围与其他字段',async t=>{
 const writes:any[]=[]
 const field={key:'energy',name:'能量',type:'number',unit:'kWh',kinds:['model'],product_line_ids:['line','shared']}
 const library={id:'shelf',updated_at:'2026-11-07',entity_schema:{kinds:[],fields:[{key:'fixed',name:'固定字段',type:'text',kinds:['part']},field]}}
 const component=loadComponent(new URL('./ShelfFieldEditor.vue',import.meta.url),{...entryPresentationTestModules,'@/api/shelf-files':{shelfWriteError:(_:unknown,f:string)=>f,configureShelf:async(...args:any[])=>{writes.push(args);return{success:true,data:{status:'confirmed'}}}}})
 const app=renderer.createApp(component,{library,field,lineId:'line'});app.use(createI18n({legacy:false,locale:'zh-CN',messages:{'zh-CN':zh,'en-US':en}}));const root=element('root');app.mount(root);t.after(()=>app.unmount());await settle()
 descendants(root).find(n=>n.tag==='input'&&n.props['aria-label']==='名称')!.props.onInput({target:{value:' 客户能量 '}});await settle()
 descendants(root).find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await settle()
 assert.equal(writes[0][1],library.updated_at);assert.deepEqual(writes[0][2].entity_schema.fields[0],library.entity_schema.fields[0]);assert.deepEqual(writes[0][2].entity_schema.fields[1].product_line_ids,['line','shared']);assert.equal(writes[0][2].entity_schema.fields[1].name,' 客户能量 ')
})
