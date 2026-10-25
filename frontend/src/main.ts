import { installImeEnterGuard } from "./utils/imeEnter";
import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import router from "./router/index";
import "./assets/fonts.css";
import TDesign from "tdesign-vue-next";
// 引入组件库的少量全局样式变量
import "tdesign-vue-next/dist/tdesign.css";
import "./assets/popup-word-wrapping.css";
import "./assets/narrow-popups.css";
import "@/assets/theme/theme.css";
import "@/assets/dropdown-menu.less";
import "@/components/css/highlight.less";
import i18n from "./i18n/index";
import { initTheme } from "@/composables/useTheme";
import { initFont } from "@/composables/useFont";
import { installTDesignIconOfflineGuard } from "@/utils/tdesign-icon-offline";
import { installAutofillGuard } from "@/utils/disable-autofill";
import { installPaginationKeyboard } from "@/utils/paginationKeyboard";

// 必须在 Vue 组件挂载之前执行，避免 tdesign-icons 运行时请求 tdesign.gtimg.com
installTDesignIconOfflineGuard();

initTheme();
initFont();

async function bootstrap() {
  const app = createApp(App);

  // 全局错误处理：捕获未处理的组件错误，防止白屏
  app.config.errorHandler = (err, instance, info) => {
    console.error("[Reliquary] Unhandled Vue error:", err, "\nComponent:", instance, "\nInfo:", info);
  };

  app.use(TDesign);
  const pinia = createPinia();
  app.use(pinia);

  app.use(router);
  app.use(i18n);

  // 等首屏路由（含导航守卫、显式 QA 自动登录）完成后再挂载，避免先闪默认页再跳转
  await router.isReady();
  const removeImeEnterGuard = installImeEnterGuard(document);
  app.onUnmount(removeImeEnterGuard);
  app.mount("#app");
  installAutofillGuard();
  const paginationLabels = { previous: "ikb19.previous", next: "ikb19.next", more: "ikb19.morePages", jump: "ikb19.jumpToPage" } as const;
  installPaginationKeyboard(key => i18n.global.t(paginationLabels[key]));
}

bootstrap();
