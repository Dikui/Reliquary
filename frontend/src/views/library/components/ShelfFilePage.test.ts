import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import * as external from '../../../utils/externalUrl'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
// Small spec, task 20 / ADR 0024: real version page with synthetic API responses.
test('版本页保留旧原件下载，并明确指定版本和解析器重解析', async t => {
 const writes: any[]=[]
 const component=loadComponent(new URL('./ShelfFilePage.vue',import.meta.url),{
  ...entryPresentationTestModules,'@/utils/externalUrl':external,
  'vue-router':{useRoute:()=>({query:{libraries:'shelf',tab:'files',file:'file'}}),useRouter:()=>({replace(){}})},
  '@/api/shelf-files':{listShelfVersions:async()=>({success:true,data:[{id:'new',file_id:'file',version_label:' V2 ',stage:'final',is_current:true,parse_status:'failed',parse_error:'Synthetic failure',attachment_id:'original2'},{id:'old',file_id:'file',version_label:' V1 ',stage:'draft',is_current:false,parse_status:'completed',entry_id:'text',attachment_id:'original1'}]}),shelfVersionDownload:(f:string,v:string)=>`/download/${f}/${v}`,shelfWriteError:(_:unknown,f:string)=>f,writeShelf:async(_:string,ops:any)=>{writes.push(ops);return{success:true,data:{status:'confirmed'}}}},
  '@/api/entities':{getEntityAttachment:async(id:string)=>({success:true,data:{id,name:'原件.pdf',size:1048576}}),getEntity:async()=>({success:true,data:{entity:{id:'file',canonical_name:' 原件名称 ',revision:5}}}),entityHistory:async()=>({success:true,data:{items:[],total:0}})},
  '@/api/library':{getEntryEditableBody:async()=>({content:'<!-- page: 1 -->\nSynthetic text'})},
  '@/components/ui/feedback':{confirm:()=>({result:Promise.resolve(true)})},
  './ShelfDocumentEditor.vue':{__esModule:true,default:defineComponent({setup:()=>()=>null})},
 })
 const app=renderer.createApp(component,{library:{id:'shelf',shelf_config:{parsers:[{key:'parser',name:'客户解析器',type:'http'}]}},fileId:'file'});app.use(createI18n({legacy:false,locale:'zh-CN',messages:{'zh-CN':zh,'en-US':en}}));const root=element('root');app.mount(root);t.after(()=>app.unmount());await settle()
 assert.match(visibleText(root),/原件名称/);assert.match(visibleText(root),/Synthetic failure/)
 assert.ok(descendants(root).some(n=>n.tag==='a'&&n.props.href==='/download/file/old'))
 const parser=descendants(root).find(n=>n.tag==='select'&&n.props['aria-label']==='解析器')!;parser.props.onChange({target:{value:'parser'}});await settle()
 await click(descendants(root).find(n=>n.tag==='button'&&visibleText(n)==='换解析器重解析')!);await settle()
 assert.equal(writes[0][0].type,'reparse_document');assert.equal(writes[0][0].revision,5);assert.deepEqual(writes[0][0].reparse,{version_id:'new',parser_key:'parser'})
})
