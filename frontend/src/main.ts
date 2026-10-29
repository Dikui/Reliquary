import { installImeEnterGuard } from "./utils/imeEnter";
import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import router from "./router/index";
import "./styles/tokens.css";
import "./styles/global.css";
import "@/components/css/highlight.less";
import i18n from "./i18n/index";
import { initTheme } from "@/composables/useTheme";
import { initFont } from "@/composables/useFont";
import { installAutofillGuard } from "@/utils/disable-autofill";


initTheme();
initFont();

async function bootstrap() {
  const app = createApp(App);

  // 全局错误处理：捕获未处理的组件错误，防止白屏
  app.config.errorHandler = (err, instance, info) => {
    console.error("[Reliquary] Unhandled Vue error:", err, "\nComponent:", instance, "\nInfo:", info);
  };

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
}

bootstrap();
