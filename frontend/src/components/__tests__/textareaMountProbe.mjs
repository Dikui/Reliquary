import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import * as vue from 'vue'
import { createI18n } from 'vue-i18n'
import RawTextarea from 'tdesign-vue-next/es/textarea/textarea.mjs'
import { loadComponent } from '../../views/knowledge/componentTestHarness.ts'
import zhCN from '../../i18n/locales/zh-CN.ts'
import enUS from '../../i18n/locales/en-US.ts'
import * as toolCapabilities from '../../utils/tool-capabilities.ts'
import * as toolNames from '../../utils/toolNames.ts'
import * as modelDefaults from '../../utils/modelDefaults.ts'
import * as readiness from '../../utils/agent-readiness.ts'
import * as navigation from '../../config/editorNavigation.ts'
import * as settingsRoutes from '../../config/settingsRoute.ts'
import * as autosize from '../../utils/textareaAutosize.ts'

const require = createRequire(import.meta.url)
class TestElement {
  constructor(tag, text='') {this.tag=tag;this.text=text;this.nodeType=1;this.value='';this.placeholder='';this.style={};this.props={};this.children=[];this.parent=null;this.focusCount=0;this.blurCount=0;this.clientWidth=500;this.hidden=false;this.offsetWidth=500}
  get scrollHeight(){return (String(this.value||'').split('\n').length)*24}
  get parentNode(){return this.parent}
  get isConnected(){return this.tag==='body' || !!this.parent?.isConnected}
  appendChild(node){if(node.parent)node.parent.removeChild(node);node.parent=this;this.children.push(node)}
  removeChild(node){this.children.splice(this.children.indexOf(node),1);node.parent=null}
  setAttribute(name,value){this[name]=value}
  getClientRects(){return this.hidden || this.style.display==='none' || this.parent?.getClientRects().length===0 ? [] : [{width:500}]}
  querySelector(selector){return descendants(this).find(node=>node.tag===selector)||null}
  querySelectorAll(selector){return descendants(this).filter(node=>node.tag===selector)}
  dispatchEvent(event){this.props['on'+event.type[0].toUpperCase()+event.type.slice(1)]?.({target:this});return true} addEventListener(){} removeEventListener(){} focus(){this.focusCount++} blur(){this.blurCount++} scrollTo(){}
}
const descendants=node=>[node,...node.children.flatMap(descendants)]
const vueWarnings=[]
const measurements=[]
const body=new TestElement('body')
const observers=new Set()
globalThis.HTMLElement=TestElement
globalThis.HTMLTextAreaElement=TestElement
globalThis.document={createElement:tag=>new TestElement(tag),body,activeElement:null,addEventListener(){},removeEventListener(){}}
globalThis.window={navigator:{userAgent:'test'},addEventListener(){},removeEventListener(){},getComputedStyle(node){
  measurements.push(node?.tag ?? null)
  if(!(node instanceof TestElement))throw new TypeError("Failed to execute 'getComputedStyle' on 'Window': parameter 1 is not of type 'Element'.")
  return {getPropertyValue(name){return name==='box-sizing'?'border-box':name==='line-height'?'24px':'0px'}}
}}
globalThis.ResizeObserver=class {constructor(callback){this.callback=callback;observers.add(this)}observe(node){this.node=node}disconnect(){observers.delete(this)}}
const renderer=vue.createRenderer({
  createElement:tag=>new TestElement(tag),createText:text=>new TestElement('#text',text),createComment:text=>new TestElement('#comment',text),
  setText(node,text){node.text=text},setElementText(node,text){node.text=text;node.children=[]},parentNode:node=>node.parent,
  nextSibling(node){return node.parent?.children[node.parent.children.indexOf(node)+1]||null},
  insert(node,parent,anchor){if(node.parent)node.parent.removeChild(node);node.parent=parent;const index=parent.children.indexOf(anchor);if(index<0)parent.children.push(node);else parent.children.splice(index,0,node)},
  remove(node){node.parent?.removeChild(node)},patchProp(node,key,old,value){node.props[key]=value;if(key==='style')Object.assign(node.style,value);else node[key]=value},querySelector:()=>body,
})
const pass=vue.defineComponent({setup:(_props,{slots})=>()=>vue.h('div',slots.default?.())})
const empty=vue.defineComponent({setup:()=>()=>null})
const vueForTemplate={...vue,Teleport:pass,Transition:pass,vShow:{beforeMount(node,{value}){node.hidden=!value},updated(node,{value}){node.hidden=!value}}}
function safeTextarea(){return loadComponent(process.env.RELIQUARY_SAFE_TEXTAREA_SOURCE ? new URL(`file://${process.env.RELIQUARY_SAFE_TEXTAREA_SOURCE}`) : new URL('../SafeTextarea.vue',import.meta.url),{'tdesign-vue-next':{Textarea:RawTextarea},'@/utils/textareaAutosize':autosize})}
const register=app=>{
  app.component('t-textarea',RawTextarea)
  for(const name of ['t-icon','t-button','t-loading','t-tooltip','t-input','t-select','t-option','t-option-group','t-switch','t-input-number','t-radio','t-radio-group','t-radio-button','t-checkbox','t-checkbox-group','t-tag','t-slider'])app.component(name,pass)
  app.config.warnHandler=warning=>vueWarnings.push(warning)
  app.use(createI18n({legacy:false,locale:process.argv[3]||'zh-CN',messages:{'zh-CN':zhCN,'en-US':enUS}}))
}
async function settle(){for(let i=0;i<30;i++)await vue.nextTick();await new Promise(resolve=>setImmediate(resolve))}
function mount(component,props){const root=new TestElement('main');body.appendChild(root);const app=renderer.createApp(vue.defineComponent({setup:()=>()=>vue.h(component,props)}));register(app);app.mount(root);return {app,root}}
const mode=process.argv[2]||'agent'
const intentValues=[]
const readonlyWarnings=[]
const originalWarn=console.warn
console.warn=(...args)=>{if(args.join(' ').includes('target is readonly'))readonlyWarnings.push(args.join(' '));else originalWarn(...args)}
if(mode==='agent' || mode==='agent-readonly'){
  const modules={
    vue:vueForTemplate,'vue-router':{useRouter:()=>({push(){}})},'tdesign-vue-next':{MessagePlugin:{success(){},warning(){},error(){}}},
    '@/utils/toolNames':toolNames,'@/config/editorNavigation':navigation,'@/utils/modelDefaults':modelDefaults,'@/utils/clipboard':{copyWithToast:async()=>{}},'@/utils/agent-readiness':readiness,'@/utils/tool-capabilities':toolCapabilities,'@/config/settingsRoute':settingsRoutes,
    '@/api/agent':{createAgent:async()=>{},updateAgent:async()=>{}},
    '@/stores/ui':{useUIStore:()=>vue.reactive({showSettingsModal:false,openSettings(){}})},
    '@/stores/chatResources':{useChatResourcesStore:()=>vue.reactive({allModels:[],rawKnowledgeBases:[],ensureModels:async()=>{},ensureKnowledgeBases:async()=>{}})},
    '@/stores/editorResources':{useEditorResourcesStore:()=>({agentTypePresets:[],promptTemplates:{intent_prompts:[{id:'search',name:'Search',content:'default intent'}]},placeholders:null,tenantRetrievalConfig:null,mcpServices:[],parserEngines:[],prefetchAgentEditorDeps:async()=>{}})},
    '@/components/AgentAvatar.vue':empty,'@/components/PromptTemplateSelector.vue':empty,'@/components/ModelSelector.vue':empty,'@/views/knowledge/settings/KBParserSettings.vue':empty,'@/utils/zoom':{getRootZoom:()=>1,rectToCssPx:value=>value},
  }
  if(require('node:fs').existsSync(new URL('../SafeTextarea.vue',import.meta.url)))modules['@/components/SafeTextarea.vue']=safeTextarea()
  const componentSource=process.env.RELIQUARY_AGENT_EDITOR_SOURCE ? new URL(`file://${process.env.RELIQUARY_AGENT_EDITOR_SOURCE}`) : new URL('../../views/agent/AgentEditorModal.vue',import.meta.url)
  const component=loadComponent(componentSource,modules)
  const props=vue.reactive({visible:false,mode:'edit',agent:{id:'built-in-quick-answer',name:'quick',is_builtin:false,config:{agent_mode:'quick-answer',system_prompt:'normal',context_template:'context'}},'onUpdate:visible':value=>{props.visible=value}})
  const {app,root}=mount(component,props)
  for(let cycle=0;cycle<3;cycle++){
    props.visible=true;await settle()
    const navItems=descendants(root).filter(node=>String(node.props.class).split(' ').includes('nav-item'))
    assert.ok(navItems.length>=4, 'all editor sections must be mounted')
    for(const nav of navItems){nav.props.onClick();await settle();assert.ok(String(nav.props.class).split(' ').includes('active'))}
    const radios=descendants(root).find(node=>node.props['onUpdate:modelValue'] && node.props.modelValue==='quick-answer')
    assert.ok(radios, 'mode selector must be mounted')
    if(radios && mode!=='agent-readonly'){radios.props['onUpdate:modelValue']('smart-reasoning');await vue.nextTick();radios.props['onUpdate:modelValue']('quick-answer');await settle()}
    if(mode==='agent-readonly'){
      const inputs=descendants(root).filter(node=>node.tag==='textarea')
      const textarea=inputs.find(node=>node.placeholder==='default intent')
      assert.ok(textarea,JSON.stringify(inputs.map(n=>({value:n.value,placeholder:n.placeholder}))))
      for(const value of ['short','']){textarea.value=value;textarea.props.onInput({target:textarea});await settle()}
      intentValues.push(textarea.value)
    }
    const textarea=descendants(root).find(node=>node.tag==='textarea')
    assert.ok(textarea, 'editor must contain an actual textarea')
    if(mode!=='agent-readonly')textarea.props.onInput({target:{value:`last input ${cycle}`}})
    props.visible=false;await settle()
  }
  app.unmount();await settle()
  assert.ok(!measurements.includes(null))
  if(mode==='agent-readonly'){console.log(JSON.stringify({readonlyWarnings:readonlyWarnings.length,warnings:readonlyWarnings}));assert.deepEqual(readonlyWarnings,[]);assert.ok(intentValues.every(value=>value==='default intent'),'clearing returns to the existing default template')}
  assert.equal(observers.size,0)
  console.log(JSON.stringify({mode,locale:process.argv[3]||'zh-CN',cycles:3,measurements:measurements.length,invalidMeasurements:measurements.filter(value=>value===null).length,resizeObserversAfterUnmount:observers.size}))
}else if(mode==='readonly'){
  const props=vue.reactive({modelValue:'old text',autosize:{minRows:2,maxRows:4},'onUpdate:modelValue':value=>{props.modelValue=value}})
  const {app,root}=mount(safeTextarea(),props)
  await settle()
  const textarea=descendants(root).find(node=>node.tag==='textarea')
  for(const value of ['short','']){textarea.value=value;textarea.props.onInput({target:textarea});await settle()}
  assert.equal(props.modelValue,'')
  textarea.props.onCompositionstart({target:textarea})
  textarea.value='中文'
  textarea.props.onInput({target:textarea})
  assert.equal(props.modelValue,'')
  textarea.props.onCompositionend({target:textarea})
  await settle()
  assert.equal(props.modelValue,'中文')
  textarea.value='pending input';textarea.props.onInput({target:textarea})
  props.modelValue='';await settle()
  assert.equal(props.modelValue,'')
  console.log(JSON.stringify({mode,readonlyWarnings:readonlyWarnings.length,warnings:readonlyWarnings}))
  assert.deepEqual(readonlyWarnings,[])
  assert.equal(textarea.value,'','an external clear wins over the queued input callback')
  props.modelValue='external replacement';await settle()
  assert.equal(textarea.value,'external replacement')
  assert.equal(descendants(root).find(node=>node.tag==='textarea'),textarea,'external edits keep the focused DOM node')
  app.unmount();await settle()
  assert.deepEqual(readonlyWarnings,[])
  assert.deepEqual(vueWarnings,[])
  console.log(JSON.stringify({mode,readonlyWarnings:readonlyWarnings.length,composedValue:'中文',externalValue:'external replacement'}))
}else if(mode==='behavior'){
  const changes=[]
  const events=[]
  const props=vue.reactive({modelValue:Array.from({length:10},()=> 'line').join('\n'),autosize:{minRows:2,maxRows:4},placeholder:'placeholder',maxlength:200,name:'prompt',status:'warning',onChange:(value,context)=>changes.push({value,context}),onKeydown:(value,context)=>events.push({value,context})})
  const safe=vue.ref()
  const component=safeTextarea()
  const parent=vue.defineComponent({setup:()=>()=>vue.h(component,{...props,ref:safe},{tips:()=>vue.h('span','textarea hint')})})
  const {app,root}=mount(parent,{})
  await settle()
  const textarea=descendants(root).find(node=>node.tag==='textarea')
  assert.ok(textarea)
  assert.ok(descendants(root).some(node=>node.text==='textarea hint'))
  assert.equal(textarea.style.height,'96px')
  assert.equal(textarea.style.minHeight,'48px')
  assert.equal(textarea.style.resize,'none')
  assert.ok(safe.value.$el.querySelector('textarea')===textarea)
  safe.value.focus();safe.value.blur()
  assert.equal(textarea.focusCount,1);assert.equal(textarea.blurCount,1)
  assert.equal(textarea.placeholder,'placeholder');assert.equal(textarea.name,'prompt');assert.equal(textarea.maxlength,200)
  const input={target:{value:'new input'}}
  textarea.props.onInput(input)
  await settle()
  assert.equal(changes.length,1);assert.equal(changes[0].value,'new input');assert.equal(changes[0].context.e,input)
  const keydown={key:'Enter'}
  textarea.props.onKeydown(keydown)
  assert.equal(events.length,1);assert.equal(events[0].context.e,keydown)
  props.modelValue='short';await settle();assert.equal(textarea.style.height,'48px')
  root.hidden=true;const beforeHidden=measurements.length
  props.modelValue=Array.from({length:10},()=> 'line').join('\n');await settle();assert.equal(measurements.length,beforeHidden)
  root.hidden=false;for(const observer of observers)observer.callback([]);await settle();assert.equal(textarea.style.height,'96px')
  app.unmount();await settle();assert.equal(observers.size,0)
  assert.equal(body.children.filter(node=>node.tag==='textarea').length,0)
  assert.ok(!measurements.includes(null))
  console.log(JSON.stringify({mode,heights:['48px','96px'],changeEvents:changes.length,focus:textarea.focusCount,blur:textarea.blurCount,invalidMeasurements:0,resizeObserversAfterUnmount:observers.size}))
}else{
  const component=mode==='original'?RawTextarea:safeTextarea()
  const visible=vue.ref(true)
  const parent=vue.defineComponent({setup(){vue.onMounted(()=>{visible.value=false});return ()=>visible.value?vue.h(component,{modelValue:'test',autosize:{minRows:2,maxRows:4}}):null}})
  const {app}=mount(parent,{})
  await settle();app.unmount();await settle()
  assert.ok(!measurements.includes(null))
  console.log(JSON.stringify({mode,invalidMeasurements:measurements.filter(value=>value===null).length}))
}
