<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import coverMotion from '../assets/cover-motion.mp4'
import coverStill from '../assets/cover-still.webp'

// Two stacked videos cross-fade near the end of each loop so the seam is not visible.
const props = defineProps<{ paused: boolean; hidden: boolean }>()
const emit = defineEmits<{ ready: [boolean] }>()
const FADE = .9
const a = ref<HTMLVideoElement | null>(null)
const b = ref<HTMLVideoElement | null>(null)
const ready = ref(false)
let current: HTMLVideoElement
let next: HTMLVideoElement
let swapping = false
let swapTimer = 0
let visible = true
let observer: IntersectionObserver | undefined
const frameCallbacks = new Map<HTMLVideoElement, number>()

const play = (video: HTMLVideoElement) => { void video.play().catch(() => {}) }
const canPlay = () => ready.value && visible && !props.paused && !props.hidden && !document.hidden

function sync() {
  if (!a.value || !b.value) return
  if (canPlay()) play(current)
  else {
    a.value.pause()
    b.value.pause()
  }
}

function markReady() {
  if (ready.value || !a.value || a.value.readyState < 2) return
  ready.value = true
  emit('ready', true)
  sync()
}

function tick() {
  if (!canPlay() || swapping || !Number.isFinite(current.duration) ||
      current.duration - current.currentTime > FADE || next.readyState < 2) return
  swapping = true
  const outgoing = current
  const incoming = next
  outgoing.loop = false
  incoming.currentTime = 0
  play(incoming)
  // Keep the lower layer opaque while the upper layer fades, in either direction.
  if (incoming === a.value) {
    incoming.classList.add('is-instant', 'is-active')
    outgoing.classList.remove('is-active')
  } else {
    incoming.classList.add('is-active')
  }
  current = incoming
  next = outgoing
  swapTimer = window.setTimeout(() => {
    outgoing.classList.remove('is-active')
    incoming.classList.remove('is-instant')
    outgoing.pause()
    outgoing.loop = true
    outgoing.currentTime = 0
    swapping = false
  }, FADE * 1000 + 60)
}

function watchFrames(video: HTMLVideoElement) {
  if (typeof video.requestVideoFrameCallback !== 'function') {
    video.addEventListener('timeupdate', tick)
    return
  }
  const frame = () => {
    if (video === current) tick()
    frameCallbacks.set(video, video.requestVideoFrameCallback(frame))
  }
  frameCallbacks.set(video, video.requestVideoFrameCallback(frame))
}

function fail() {
  ready.value = false
  emit('ready', false)
}

watch(() => [props.paused, props.hidden], sync)

onMounted(() => {
  if (!a.value || !b.value) return
  current = a.value
  next = b.value
  a.value.addEventListener('loadeddata', markReady)
  a.value.addEventListener('error', fail)
  watchFrames(a.value)
  watchFrames(b.value)
  observer = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting
    sync()
  })
  observer.observe(a.value.parentElement!)
  document.addEventListener('visibilitychange', sync)
  markReady()
})

onBeforeUnmount(() => {
  observer?.disconnect()
  document.removeEventListener('visibilitychange', sync)
  clearTimeout(swapTimer)
  for (const [video, id] of frameCallbacks) video.cancelVideoFrameCallback(id)
  for (const video of [a.value, b.value]) {
    video?.removeEventListener('timeupdate', tick)
    video?.pause()
  }
})
</script>

<template>
  <img class="bg-still" :src="coverStill" alt="">
  <video ref="a" class="bg-video is-active" :src="coverMotion" :poster="coverStill" muted loop playsinline
    preload="auto" disablepictureinpicture></video>
  <video ref="b" class="bg-video" :src="coverMotion" muted loop playsinline preload="auto"
    disablepictureinpicture></video>
</template>
