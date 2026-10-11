import assert from 'node:assert/strict'
import { safeTextareaTestModule } from '../../../components/__tests__/safeTextareaStub'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import * as vue from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { shouldRejectKnowledgeFileType } from '../../../utils/fileTypeVerification'
import { loadComponent, renderer, element, descendants, visibleText, registerPrimitives, settle } from '../componentTestHarness'

const require = createRequire(import.meta.url)
const pass = vue.defineComponent({ setup: (_props, { slots }) => () => vue.h('div', slots.default?.()) })
const nullComponent = vue.defineComponent({ setup: () => () => null })

function loadFilter() {
  const source = readFileSync(new URL('../utils/uploadSources.ts', import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils' ? { fileSizeVerification: (file: File) => file.size > 50 * 1024 * 1024 } : name === '@/utils/fileTypeVerification' ? { shouldRejectKnowledgeFileType } : require(name), module, module.exports)
  return module.exports
}
const { filterUploadFiles, filterUnsupportedAudioFiles } = loadFilter()

test('upload filtering separates unavailable PDF/Office from unsupported HTML and oversized files', () => {
  const files = ['sample.pdf', 'sample.docx', 'page.html', 'notes.md'].map(name => ({ name, size: 12 }))
  const result = filterUploadFiles(files, { supportedFileTypes: ['md', 'png'] })
  assert.deepEqual(result.validFiles.map((file: File) => file.name), ['notes.md'])
  assert.equal(result.unavailableEngineCount, 2)
  assert.equal(result.unsupportedTypeCount, 1)
  assert.equal(filterUploadFiles([{name: 'page.html', size: 1}], {supportedFileTypes: ['md']}).unavailableEngineCount, 0)
  assert.equal(filterUploadFiles([{name: 'large.md', size: 51 * 1024 * 1024}], {supportedFileTypes: ['md']}).oversizedCount, 1)
})

for (const locale of ['zh-CN', 'en-US']) {
  const i18n = () => createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  test(`${locale}: upload entry explains how to restore PDF/Office support and only rejects HTML as unsupported`, async () => {
    const warnings: string[] = []
    const component = loadComponent(new URL('./KbUploadSourceDropdown.vue', import.meta.url), {
      '../utils/uploadSources': { filterUploadFiles },
      'tdesign-vue-next': { MessagePlugin: {warning: (message: any) => warnings.push(typeof message === 'string' ? message : message.content)}, Icon: nullComponent },
    })
    const root = element('main')
    const app = renderer.createApp(component, {supportedFileTypes: ['md', 'png']})
    app.use(i18n()); registerPrimitives(app); app.mount(root)
    const input = descendants(root).find(node => node.tag === 'input')!
    input.props.onChange({target: {files: [{name:'sample.pdf',size:1}],value:'x'}})
    assert.equal(warnings.length,1)
    for (const word of ['Docker', 'make anydoc-lib', '-tags anydoc', 'MinerU']) assert.ok(warnings[0].includes(word))
    warnings.length=0
    input.props.onChange({target: {files: [{name:'page.html',size:1}],value:'x'}})
    assert.equal(warnings.length,1)
    assert.equal(warnings[0],i18n().global.t('error.unsupportedFileType'))
    app.unmount()
  })

  {
    {
      test(`${locale}: PNG uploads preserve source metadata with article processing`, async () => {
        const confirmations: any[] = []
        const component = loadComponent(new URL('./UploadConfirmDialog.vue', import.meta.url), {
          '@/components/SafeTextarea.vue': safeTextareaTestModule,
          vue: {...vue, Teleport: pass, Transition: pass, vShow: {beforeMount() {}, updated() {}}},
          'tdesign-vue-next': {MessagePlugin:{warning(){}}},
          '@/components/ModelSelector.vue': nullComponent,
          '../settings/KBParserSettings.vue': nullComponent,
          './KbUploadSourceDropdown.vue': nullComponent,
          '@/stores/resources': {useResourcesStore:()=>({ensureModels:async()=>{},allModels:[]})},
          '@/stores/editorResources': {useEditorResourcesStore:()=>({parserEngines:[]})},
          '@/stores/ui': {useUIStore:()=>({openSettings(){}})},
          '@/utils/files': {formatFileSize:()=> '1 KB',getFileIcon:()=> 'file'},
          '../utils/uploadSources': {getUploadFileKey:(file:File)=>file.name},
          '@/api/knowledge-base': {listKnowledgeTags:async()=>({data:{data:[]}})},
        })
        const props=vue.reactive({visible:false,mode:'file',files:[{name:'sample.png',size:100}],kbInfo:{id:'kb'},onConfirm:(result:any)=>confirmations.push(result)})
        const root=element('main');const app=renderer.createApp(vue.defineComponent({setup:()=>()=>vue.h(component,props)}))
        app.use(i18n());registerPrimitives(app)
        app.config.warnHandler=()=>{}
        for(const name of ['t-input','t-textarea']) app.component(name,vue.defineComponent({props:['modelValue'],setup:(props,{attrs})=>()=>vue.h(name==='t-input'?'input':'textarea',{...attrs,value:props.modelValue})}))
        for(const tag of ['t-select','t-option','t-switch','t-input-number','t-tag','t-loading','t-tooltip','t-checkbox']) app.component(tag,pass)
        app.mount(root);props.visible=true;await settle()
        const buttons=descendants(root).filter(node=>node.tag==='button')
        const footer=descendants(root).find(node=>node.tag==='footer')!
        const confirm=descendants(footer).filter(node=>node.tag==='button').at(-1)!
        assert.ok(confirm);assert.equal(confirm.props.disabled,false)
        const labels=descendants(root).filter(node=>node.tag==='label' && node.props.class==='intake-source-field')
        assert.equal(labels.length,4)
        const values=['Ada Lovelace','Personal documents','https://example.com/documents/original-image.png','2026-10-09T12:34:00Z']
        for(const [index,label] of labels.entries()){
          const input=descendants(label).find(node=>(node.tag==='input' || node.tag==='textarea'))!
          input.props['onUpdate:modelValue'](values[index])
        }
        await settle()
        for(const [index,label] of labels.entries()) assert.equal(descendants(label).find(node=>(node.tag==='input' || node.tag==='textarea'))!.props.value,values[index])
        for(const key of ['intakeAuthor','intakePublisher','intakeOriginalUrl','intakePublishedAt']) assert.ok(labels.some(node=>visibleText(node).includes(i18n().global.t(`knowledgeBase.${key}`))))
        confirm.props.onClick();await settle()
        assert.equal(confirmations.length,1)
        assert.deepEqual(Object.keys(confirmations[0].processConfig).sort(), ['chunking_config', 'parser_engine_rules'])
        app.unmount()
      })
    }
  }

  test(`${locale}: empty state and drag mask use available types and runtime size`, async()=>{
    for(const name of ['EmptyKnowledge.vue','UploadMask.vue']){
      const component=loadComponent(new URL(`../../../components/${name}`,import.meta.url),{'@/utils':{MAX_FILE_SIZE_MB:75},'@/assets/img/upload.svg':'upload.svg','@/assets/img/upload-mask.svg':'mask.svg'})
      const root=element('main');const app=renderer.createApp(component,{supportedFileTypes:['md','png']});app.use(i18n());app.mount(root)
      const text=visibleText(root)
      assert.ok(text.includes('.md, .png'));assert.ok(text.includes('75 MB'));assert.ok(!text.includes('pdf'));assert.ok(!text.includes('200K'))
      app.unmount()
    }
  })
}

test('desktop source fields keep full labels and one full-width input per row', async () => {
  const less = require('less')
  const { parse } = await import('@vue/compiler-sfc')
  const source = readFileSync(new URL('./UploadConfirmDialog.vue',import.meta.url),'utf8')
  const { descriptor }=parse(source)
  const {css}=await less.render(descriptor.styles[0].content)
  const panel=css.match(/\.files-panel\s*\{([^}]+)\}/)![1]
  assert.match(panel,/width:\s*clamp\(280px, 28vw, 360px\)/)
  const fields=css.match(/\.intake-source-fields\s*\{([^}]+)\}/)![1]
  assert.match(fields,/grid-template-columns:\s*minmax\(0, 1fr\)/)
  assert.match(fields,/box-sizing:\s*border-box/)
  assert.match(css,/\.intake-source-field span\s*\{[^}]*white-space:\s*normal/)
})


test('audio rejection precedes engine availability for every supported audio suffix', () => {
  const files = ['MP3', 'wav', 'm4a', 'flac', 'ogg', 'aac'].map(ext => ({ name: `audio.${ext}`, size: 1 }))
  for (const supportedFileTypes of [undefined, [], ['md'], ['mp3', 'wav', 'm4a', 'flac', 'ogg', 'aac']]) {
    const result = filterUploadFiles(files, { supportedFileTypes })
    assert.equal(result.validFiles.length, 0)
    assert.equal(result.audioFilteredCount, files.length)
    assert.equal(result.unavailableEngineCount, 0)
    assert.equal(result.unsupportedTypeCount, 0)
  }
})

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: file and folder selections reject audio without parser recovery advice`, async () => {
    const warnings: string[] = []
    const emitted: File[][] = []
    const component = loadComponent(new URL('./KbUploadSourceDropdown.vue', import.meta.url), {
      '../utils/uploadSources': { filterUploadFiles },
      'tdesign-vue-next': { MessagePlugin: { warning: (message: any) => warnings.push(typeof message === 'string' ? message : message.content) }, Icon: nullComponent },
    })
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    const root = element('main')
    const app = renderer.createApp(component, { supportedFileTypes: ['md'], onFiles: (files: File[]) => emitted.push(files) })
    app.use(i18n); registerPrimitives(app); app.mount(root)
    const inputs = descendants(root).filter(node => node.tag === 'input')
    assert.equal(inputs.length, 2)
    for (const input of inputs) {
      warnings.length = 0
      input.props.onChange({ target: { files: [{ name: 'sample.mp3', size: 1, webkitRelativePath: 'folder/sample.mp3' }], value: 'x' } })
      assert.deepEqual(warnings, [i18n.global.t('error.unsupportedAudioUpload')])
      assert.equal(emitted.length, 0)
      for (const word of ['Docker', 'anydoc', 'MinerU']) assert.ok(!warnings[0].includes(word))
      warnings.length = 0
      input.props.onChange({ target: { files: [{ name: 'sample.pdf', size: 1, webkitRelativePath: 'folder/sample.pdf' }], value: 'x' } })
      assert.deepEqual(warnings, [i18n.global.t('knowledgeBase.parserRecoveryHint')])
    }
    app.unmount()
  })

  test(`${locale}: the knowledge page rejects dropped audio before opening upload confirmation`, () => {
    const source = readFileSync(new URL('../KnowledgeBase.vue', import.meta.url), 'utf8')
    const handler = source.match(/const handleUploadSourceFiles = \(files: File\[\]\) => \{[\s\S]*?\n\};/)![0]
    const code = ts.transpileModule(handler, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    const warnings: string[] = []
    const confirmations: File[][] = []
    const invoke = new Function('filterUnsupportedAudioFiles', 'MessagePlugin', 't', 'ensureDocumentKbReady', 'openUploadConfirmDialog', code + '\nreturn handleUploadSourceFiles;')(
      filterUnsupportedAudioFiles, { warning: (message: string) => warnings.push(message) },
      i18n.global.t, () => true, (files: File[]) => confirmations.push(files),
    )
    invoke([{ name: 'sample.mp3', size: 1 }])
    assert.deepEqual(warnings, [i18n.global.t('error.unsupportedAudioUpload')])
    assert.equal(confirmations.length, 0)
    warnings.length = 0
    const markdown = { name: 'notes.md', size: 1 }
    invoke([{ name: 'sample.wav', size: 1 }, markdown])
    assert.deepEqual(warnings, [i18n.global.t('error.unsupportedAudioUpload')])
    assert.deepEqual(confirmations, [[markdown]])
  })
}
