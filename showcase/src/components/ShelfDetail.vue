<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { certificationStatus, checklistTone, type ShelfRow } from '../data/shelf'

const props = defineProps<{ row: ShelfRow; market: string; compared: boolean; compareCount: number }>()
const emit = defineEmits<{ back: []; toggle: [] }>()

const previewCount = 5
const docsExpanded = ref(false)
const certsExpanded = ref(false)
const checklist = computed(() => props.row.model.checklist)
const certifications = computed(() => props.row.model.certifications)
const visibleDocs = computed(() => docsExpanded.value ? checklist.value : checklist.value.slice(0, previewCount))
const visibleCerts = computed(() => certsExpanded.value ? certifications.value : certifications.value.slice(0, previewCount))

async function openList(id: 'docs' | 'certs') {
  if (id === 'docs') docsExpanded.value = true
  else certsExpanded.value = true
  await nextTick()
  const section = document.getElementById(id)
  section?.scrollIntoView({ block: 'start' })
  section?.focus({ preventScroll: true })
}
</script>

<template>
  <a class="back" :href="market ? '#map-result-title' : '#catalog-title'" @click.prevent="emit('back')">
    <span class="arw" aria-hidden="true"></span>{{ market ? '返回' + market + '货架' : '返回产品清单' }}
  </a>
  <section class="product-summary panel" aria-labelledby="detail-title">
    <p v-if="row.model.product_name" class="product-name">{{ row.model.product_name }}</p>
    <h1 id="detail-title" tabindex="-1">{{ row.model.name }}</h1>
    <dl class="meta">
      <div><dt>适用国家或地区</dt><dd>{{ row.regionLabel }}</dd></div>
      <div><dt>量产节点（GA）</dt><dd><span class="badge" :class="row.status.tone">{{ row.statusLabel }}</span></dd></div>
      <div><dt>当前阶段</dt><dd>{{ row.model.current_stage || '未记录' }}</dd></div>
      <div v-if="row.model.platform"><dt>产品平台</dt><dd>{{ row.model.platform }}</dd></div>
      <div v-if="row.model.product_line.name"><dt>产品线</dt><dd>{{ row.model.product_line.name }}</dd></div>
    </dl>
    <div v-if="row.sale?.notes || row.sale?.conditional || row.sale?.planned_eom" class="sale-notes">
      <span v-if="row.sale.conditional" class="badge purple">有适用条件</span>
      <span v-if="row.sale.planned_eom" class="badge amber">计划退市</span>
      <p v-if="row.sale.notes">{{ row.sale.notes }}</p>
    </div>
    <div class="next-actions" aria-label="下一步">
      <a class="next-primary" href="#docs" @click.prevent="openList('docs')">查看资料清单<span class="arw" aria-hidden="true"></span></a>
      <a href="#certs" @click.prevent="openList('certs')">核对认证清单<span class="arw" aria-hidden="true"></span></a>
      <button type="button" :disabled="!compared && compareCount >= 3" @click="emit('toggle')">
        {{ compared ? '移出对比' : '加入对比' }}（{{ compareCount }}/3）
      </button>
    </div>
  </section>
  <div class="detail-grid">
    <section id="docs" class="panel detail-card" aria-labelledby="docs-title" tabindex="-1">
      <div class="panel-head"><h2 id="docs-title">资料清单</h2><span class="count">{{ checklist.length }} 项</span></div>
      <p v-if="!checklist.length" class="no-data">暂无该型号的资料清单。</p>
      <table v-else id="docs-list">
        <thead><tr><th scope="col">资料名称</th><th scope="col">资料状态</th><th scope="col">文件</th></tr></thead>
        <tbody>
          <tr v-for="(item, index) in visibleDocs" :key="index">
            <td>{{ item.name }}</td><td><span class="badge" :class="checklistTone(item)">{{ item.status_label || '未记录' }}</span></td>
            <td>{{ item.has_file ? '有' : '无' }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="checklist.length > previewCount" class="button expand" type="button" :aria-expanded="docsExpanded" aria-controls="docs-list"
        @click="docsExpanded = !docsExpanded">{{ docsExpanded ? '收起清单' : '展开全部 ' + checklist.length + ' 项' }}</button>
    </section>
    <section id="certs" class="panel detail-card" aria-labelledby="certs-title" tabindex="-1">
      <div class="panel-head"><h2 id="certs-title">认证清单</h2><span class="count">{{ certifications.length }} 项</span></div>
      <p v-if="!certifications.length" class="no-data">暂无该型号的认证清单。</p>
      <table v-else id="certs-list">
        <thead><tr><th scope="col">认证名称</th><th scope="col">取证状态</th><th scope="col">适用地区</th></tr></thead>
        <tbody>
          <tr v-for="(item, index) in visibleCerts" :key="index">
            <td>{{ item.standard_name }}</td>
            <td><span class="badge" :class="certificationStatus(item.status).tone">{{ certificationStatus(item.status).label }}</span></td>
            <td>{{ item.region_name || '—' }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="certifications.length > previewCount" class="button expand" type="button" :aria-expanded="certsExpanded" aria-controls="certs-list"
        @click="certsExpanded = !certsExpanded">{{ certsExpanded ? '收起清单' : '展开全部 ' + certifications.length + ' 项' }}</button>
    </section>
  </div>
</template>
