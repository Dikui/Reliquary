import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as labels from '../utils/entrySourceLabels'
import * as plain from '../utils/markdownPlainText'
import { loadComponent, renderer, element, descendants, registerPrimitives, settle, visibleText } from '../views/library/componentTestHarness'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: updated record has an encoded latest link without triggering the parent`, async () => {
    const i18n=createI18n({legacy:false,locale,messages:{'zh-CN':zh,'en-US':en}})
    const notice=loadComponent(new URL('./EntryUpdateNotice.vue',import.meta.url),{'vue-i18n':{useI18n:()=>i18n.global}})
    const props=ref({hasUpdate:true,latestEntryId:'latest /?#'})
    const app=renderer.createApp(defineComponent({setup:()=>()=>h(notice,props.value)}));app.use(i18n)
    const root=element('root');app.mount(root);await settle()
    assert.ok(visibleText(root).includes(i18n.global.t('ikb18.hasUpdate')))
    const link=descendants(root).find(n=>n.tag==='a')!
    assert.equal(link.props.href,'/platform/entries?entry_id=latest%20%2F%3F%23')
    let stopped=false;link.props.onClick({stopPropagation(){stopped=true}});assert.equal(stopped,true)
    props.value={hasUpdate:false,latestEntryId:'latest'};await settle();assert.equal(descendants(root).filter(n=>n.tag==='a').length,0)
    props.value={hasUpdate:true,latestEntryId:''};await settle();assert.ok(visibleText(root).includes(i18n.global.t('ikb18.hasUpdate')));assert.equal(descendants(root).filter(n=>n.tag==='a').length,0)
    app.unmount()
  })
  for (const kind of ['Card','List','Search'] as const) test(`${locale}: ${kind} results pass update metadata to the rendered latest link`, async () => {
    const i18n=createI18n({legacy:false,locale,messages:{'zh-CN':zh,'en-US':en}})
    const empty={__esModule:true,default:defineComponent({setup:()=>()=>null})}
    const modules:Record<string,unknown>={'vue-i18n':{useI18n:()=>i18n.global},'@/utils/entrySourceLabels':labels,'@/utils/markdownPlainText':plain,'@/utils/files':{formatFileSize:()=>'',getFileIcon:()=>''},'@/assets/img/more-actions.png':{__esModule:true,default:''},'./GlobalCommandPalette/useHighlight':{highlightText:(v:string)=>v}}
    const url=kind==='Search'?new URL('./SearchEntry.vue',import.meta.url):new URL(`../views/library/components/Document${kind}View.vue`,import.meta.url)
    for(const m of readFileSync(url,'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g))modules[m[1]]=empty
    const notice={__esModule:true,default:loadComponent(new URL('./EntryUpdateNotice.vue',import.meta.url),modules)}
    modules['@/components/EntryUpdateNotice.vue']=notice;modules['./EntryUpdateNotice.vue']=notice
    const component=loadComponent(url,modules)
    const old={id:'old',entry_id:'old',title:'Old source',file_name:'Old source',parse_status:'completed',source:'mine',source_kind:'mine',has_update:true,latest_article_id:'latest',entities:[],topics:[],snippets:[],published_at:null,score:1}
    const props=kind==='Search'?{article:old,query:''}:{items:[old],selectedIds:new Set(),batchMode:false,traceAvailableById:{},traceVisibleIds:{}}
    const app=renderer.createApp(component,props);app.use(i18n);registerPrimitives(app)
    const errors:unknown[]=[];app.config.errorHandler=e=>errors.push(e)
    const root=element('root');app.mount(root);await settle()
    assert.deepEqual(errors,[])
    assert.equal(descendants(root).find(n=>n.tag==='a')?.props.href,'/platform/entries?entry_id=latest')
    app.unmount()
  })
}
