<script setup lang="ts">
import ImagePreview from './ImagePreview.vue'

const props = defineProps(['reviewImg', 'reviewUrl'])
const emit = defineEmits(['closePreImg'])
const close = () => {
    emit('closePreImg')
}
</script>
<template>
    <ImagePreview :visible="!!reviewImg" @close="close"
        :images="reviewUrl ? [reviewUrl] : []">
    </ImagePreview>
</template>
<style scoped lang="less"></style>
