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

// Temporary outages keep the session and retry a read-only availability check.
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
    const signedIn = Boolean(localStorage.getItem('reliquary_token'));
    const data: any = await instance.get(signedIn ? '/api/v1/auth/me' : '/health');
    if (signedIn || data?.queue_connection?.state !== 'reconnecting') serviceAvailable();
  } catch { /* The response interceptor retains the outage and session state. */ }
  finally { checkingAvailability = false; }
}
async function expireSession() {
  const { useAuthStore } = await import('@/stores/auth');
  useAuthStore().logout();
  redirectToLogin();
}

// 获取当前用户语言（用于 Accept-Language header）
export function getCurrentLanguage(): string {
  return i18n.global.locale?.value || localStorage.getItem('locale') || 'zh-CN'
}


instance.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('reliquary_token');
    if (token) {
      config.headers["Authorization"] = `Bearer ${token}`;
    }


    // 添加用户语言偏好
    config.headers["Accept-Language"] = getCurrentLanguage();

    config.headers["X-Request-ID"] = `${generateRandomString(12)}`;
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Token刷新标志，防止多个请求同时刷新token
let isRefreshing = false;
let failedQueue: Array<{ resolve: Function; reject: Function }> = [];

const PUBLIC_AUTH_PATHS = ['/auth/login', '/auth/register'];

function isPublicAuthRequest(url?: string): boolean {
  if (!url) return false;
  return PUBLIC_AUTH_PATHS.some(p => url.includes(p));
}

// 处理队列中的请求
const processQueue = (error: any, token: string | null = null) => {
  failedQueue.forEach(({ resolve, reject }) => {
    if (error) {
      reject(error);
    } else {
      resolve(token);
    }
  });

  failedQueue = [];
};

function redirectToLogin() {
  if (typeof window === 'undefined') return;
  if (window.location.pathname === '/login') return;
  window.location.href = '/login';
}

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

    if ((error.response.status === 401 || error.response.status === 403) && isPublicAuthRequest(originalRequest?.url)) {
      const { status, data } = error.response;
      const msg = typeof data === 'object'
        ? (typeof data?.error === 'string' ? data.error : (data?.error?.message || data?.message))
        : data;
      return Promise.reject({ status, message: translatePersonalServerMessage(msg, t) || t('error.invalidCredentials') });
    }

    // Embed 调试页/挂件：无 JWT 时直接拒绝，勿走 refresh → /login

    // 如果是401错误且不是刷新token的请求，尝试刷新token
    if (error.response.status === 401 && !originalRequest._retry && !originalRequest.url?.includes('/auth/refresh')) {
      if (isRefreshing) {
        // 如果正在刷新token，将请求加入队列
        return new Promise((resolve, reject) => {
          failedQueue.push({ resolve, reject });
        }).then(token => {
          originalRequest.headers['Authorization'] = 'Bearer ' + token;
          return instance(originalRequest);
        }).catch(err => {
          return Promise.reject(err);
        });
      }

      originalRequest._retry = true;
      isRefreshing = true;

      const refreshToken = localStorage.getItem('reliquary_refresh_token');

      if (refreshToken) {
        try {
          // 动态导入refresh token API
          const { refreshToken: refreshTokenAPI } = await import('../api/auth/index');
          const response = await refreshTokenAPI(refreshToken);

          if (response.success && response.data) {
            const { token, refreshToken: newRefreshToken } = response.data;

            // 更新localStorage中的token
            localStorage.setItem('reliquary_token', token);
            localStorage.setItem('reliquary_refresh_token', newRefreshToken);

            // 更新请求头
            originalRequest.headers['Authorization'] = 'Bearer ' + token;

            // 处理队列中的请求
            processQueue(null, token);

            return instance(originalRequest);
          } else {
            throw { status: response.status, message: response.message || t('error.tokenRefreshFailed') };
          }
        } catch (refreshError) {
          processQueue(refreshError, null);
          if ((refreshError as any)?.status === 401) await expireSession();

          return Promise.reject(refreshError);
        } finally {
          isRefreshing = false;
        }
      } else {
        isRefreshing = false;
        await expireSession();

        return Promise.reject({ message: t('error.pleaseRelogin') });
      }
    }

    if (error.response.status === 401 && !isPublicAuthRequest(originalRequest?.url) && !originalRequest?.url?.includes('/auth/refresh')) {
      await expireSession();
    }

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
