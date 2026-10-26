// src/utils/request.js
import axios from "axios";
import { ref } from "vue";
import { translatePersonalServerMessage } from './personalServerError';
import { generateRandomString, MAX_FILE_SIZE_MB } from "./index";
import i18n from '@/i18n'
import { getApiBaseUrl } from './api-base';

const t = (key: string) => i18n.global.t(key)

// API基础URL
const BASE_URL = getApiBaseUrl();


// 创建Axios实例
const instance = axios.create({
  baseURL: BASE_URL, // 使用配置的API基础URL
  timeout: 30000, // 请求超时时间
  headers: {
    "Content-Type": "application/json",
    "X-Request-ID": `${generateRandomString(12)}`,
  },
});

// Temporary outages retain workspace data and retry a read-only availability check.
export const serviceUnavailable = ref(false);
let availabilityTimer: ReturnType<typeof setInterval> | undefined;
let checkingAvailability = false;
export function stopServiceAvailabilityRetry() {
  if (availabilityTimer) clearInterval(availabilityTimer);
  availabilityTimer = undefined;
}
function serviceAvailable() {
  serviceUnavailable.value = false;
  stopServiceAvailabilityRetry();
}
function markServiceUnavailable() {
  serviceUnavailable.value = true;
  if (!availabilityTimer) availabilityTimer = setInterval(retryServiceAvailability, 5000);
}
export async function retryServiceAvailability() {
  if (checkingAvailability) return;
  checkingAvailability = true;
  try {
    await instance.get('/api/v1/space');
    serviceAvailable();
  } catch { /* The response interceptor retains the outage state. */ }
  finally { checkingAvailability = false; }
}

// 获取当前用户语言（用于 Accept-Language header）
export function getCurrentLanguage(): string {
  return i18n.global.locale?.value || localStorage.getItem('locale') || 'zh-CN'
}


instance.interceptors.request.use(
  (config) => {
    // 添加用户语言偏好
    config.headers["Accept-Language"] = getCurrentLanguage();

    config.headers["X-Request-ID"] = `${generateRandomString(12)}`;
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

instance.interceptors.response.use(
  (response) => {
    // 根据业务状态码处理逻辑
    const { status, data } = response;
    if (status >= 200 && status < 300) {
      if (response.config?.url !== '/health') serviceAvailable();
      return data;
    } else {
      return Promise.reject(data);
    }
  },
  async (error: any) => {
    const originalRequest = error.config;

    if (!error.response) {
      if (error.code !== 'ERR_CANCELED') markServiceUnavailable();
      return Promise.reject({ code: error.code, message: t('error.networkError') });
    }
    if ([502, 503, 504].includes(error.response.status) && error.response.data?.error?.code !== 'chatgpt_generation_paused') markServiceUnavailable();

    // 处理 Nginx 413 Request Entity Too Large
    if (error.response.status === 413) {
      return Promise.reject({
        status: 413,
        message: i18n.global.t('error.fileSizeExceeded', { size: MAX_FILE_SIZE_MB }),
        success: false
      });
    }

    const { status, data } = error.response;
    // 将HTTP状态码一并抛出，方便上层判断401等场景
    // 后端返回格式: { success: false, error: { code, message, details } }
    // 提取 error.message 作为顶层 message，方便前端使用 error?.message 获取
    let errorMessage: string | undefined;
    if (typeof data === 'object') {
      if (typeof data?.error === 'string') {
        errorMessage = data.error;
      } else if (data?.error?.message) {
        errorMessage = data.error.message;
      } else {
        errorMessage = data?.message;
      }
    } else if (typeof data === 'string') {
      errorMessage = data;
    }
    return Promise.reject({
      status,
      ...(typeof data === 'object' ? data : {}),
      message: translatePersonalServerMessage(errorMessage, t, data?.error?.code ?? data?.code)
    });
  }
);

export function get<T = any>(url: string, config?: any): Promise<T> {
  return instance.get<T>(url, config) as unknown as Promise<T>;
}

export async function getDown(url: string): Promise<Blob> {
  const res = await instance.get<Blob>(url, {
    responseType: "blob",
  }) as unknown as Blob;
  return res
}

export function postUpload(
  url: string,
  data = {},
  onUploadProgress?: (progressEvent: any) => void,
  config: any = {},
): Promise<any> {
  return instance.post(url, data, {
    ...config,
    headers: {
      "Content-Type": "multipart/form-data",
      "X-Request-ID": `${generateRandomString(12)}`,
      ...(config.headers || {}),
    },
    onUploadProgress: onUploadProgress || config.onUploadProgress,
  }) as unknown as Promise<any>;
}

export function post<T = any>(url: string, data = {}, config?: any): Promise<T> {
  return instance.post<T>(url, data, config) as unknown as Promise<T>;
}

export function put<T = any>(url: string, data = {}, config?: any): Promise<T> {
  return instance.put<T>(url, data, config) as unknown as Promise<T>;
}

export function patch<T = any>(url: string, data = {}, config?: any): Promise<T> {
  return instance.patch<T>(url, data, config) as unknown as Promise<T>;
}

export function del<T = any>(url: string, data?: any): Promise<T> {
  return instance.delete<T>(url, { data }) as unknown as Promise<T>;
}
