import { get, post, put } from '../../utils/request';
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

// GET /initialization/config/:kbId exposes credential presence, not values.
export interface ModelCredentialStatus {
    apiKey?: boolean;
}

// 初始化配置数据类型
export interface InitializationConfig {
    llm: {
        source: string;
        modelName: string;
        baseUrl?: string;
        /** @deprecated Use credentials.apiKey from GET responses */
        apiKey?: string;
        credentials?: ModelCredentialStatus;
    };
    embedding: {
        source: string;
        modelName: string;
        baseUrl?: string;
        /** @deprecated Use credentials.apiKey from GET responses */
        apiKey?: string;
        dimension?: number; // 添加embedding维度字段
        credentials?: ModelCredentialStatus;
    };
    rerank: {
        modelName: string;
        baseUrl: string;
        /** @deprecated Use credentials.apiKey from GET responses */
        apiKey?: string;
        enabled: boolean;
        credentials?: ModelCredentialStatus;
    };
    multimodal: {
        enabled: boolean;
        vlm?: {
            modelName: string;
            baseUrl: string;
            /** @deprecated Use credentials.apiKey from GET responses */
            apiKey?: string;
            interfaceType?: string; // "ollama" or "openai"
            credentials?: ModelCredentialStatus;
        };
    };
    documentSplitting: {
        chunkSize: number;
        chunkOverlap: number;
        separators: string[];
        // Adaptive chunking strategy. Empty / "legacy" = classic recursive splitter.
        // "auto" lets the backend profiler pick a tier; "heading" / "heuristic"
        // pin the tier explicitly. See backend chunker package for details.
        strategy?: string;
        // Cap chunk size in approx tokens. 0 = char-based budget only.
        tokenLimit?: number;
        // Language hints for heuristic patterns ("de", "en", "zh"). Empty = auto-detect.
        languages?: string[];
    };
    // Frontend-only hint for storage selection UI
}

// 下载任务状态类型
export interface DownloadTask {
    id: string;
    modelName: string;
    status: 'pending' | 'downloading' | 'completed' | 'failed';
    progress: number;
    message: string;
    startTime: string;
    endTime?: string;
}

// 简化版知识库配置更新接口（只传模型ID）
export interface KBModelConfigRequest {
    llmModelId: string
    embeddingModelId: string
    vlm_config?: {
        enabled: boolean
        model_id?: string
        description_language?: string
        custom_instructions?: string
    }
    documentSplitting: {
        chunkSize: number
        chunkOverlap: number
        separators: string[]
        parserEngineRules?: {
            file_types: string[]
            engine: string
        }[]
        enableParentChild?: boolean
        parentChunkSize?: number
        childChunkSize?: number
        // Adaptive chunking strategy ("auto" | "heading" | "heuristic" | "legacy").
        // The backend uses pointer-based DTOs for these three fields:
        // - undefined / not set in payload → no change on server
        // - "" / 0 / [] explicitly sent     → clears the value
        // Send the field whenever the user has opened the editor — even
        // empty values — so the user can always reset back to defaults.
        strategy?: string
        // Approximate token budget per chunk; 0 = char-based.
        tokenLimit?: number
        // Language hints for heuristic patterns. Empty array = auto-detect.
        languages?: string[]
        tableMetadataInstructions?: string
    }
    multimodal: {
        enabled: boolean
    }
    questionGeneration?: {
        enabled: boolean
        questionCount: number
        customInstructions?: string
    }
}

export function updateKBConfig(kbId: string, config: KBModelConfigRequest): Promise<any> {
    return new Promise((resolve, reject) => {
        console.log('Starting KB config update (simplified)...', kbId, config);
        put(`/api/v1/initialization/config/${kbId}`, config)
            .then((response: any) => {
                console.log('KB config update completed', response);
                resolve(response);
            })
            .catch((error: any) => {
                console.error('Failed to update KB config:', error);
                reject(error);
            });
    });
}

// 根据知识库ID执行配置更新（旧版，保留兼容性）
export function initializeSystemByKB(kbId: string, config: InitializationConfig): Promise<any> {
    return new Promise((resolve, reject) => {
        console.log('Starting KB config update...', kbId, config);
        post(`/api/v1/initialization/initialize/${kbId}`, config)
            .then((response: any) => {
                console.log('KB config update completed', response);
                resolve(response);
            })
            .catch((error: any) => {
                console.error('Failed to update KB config:', error);
                reject(error);
            });
    });
}

// 检查Ollama服务状态
export function checkOllamaStatus(): Promise<{ available: boolean; version?: string; error?: string; baseUrl?: string }> {
    return new Promise((resolve, reject) => {
        get('/api/v1/initialization/ollama/status')
            .then((response: any) => {
                resolve(response.data || { available: false });
            })
            .catch((error: any) => {
                console.error('Failed to check Ollama status:', error);
                resolve({ available: false, error: error.message || t('error.initialization.checkFailed') });
            });
    });
}

// Ollama 模型详细信息接口
export interface OllamaModelInfo {
    name: string;
    size: number;
    digest: string;
    modified_at: string;
}

// 列出已安装的 Ollama 模型（详细信息）
export function listOllamaModels(): Promise<OllamaModelInfo[]> {
    return new Promise((resolve, reject) => {
        get('/api/v1/initialization/ollama/models')
            .then((response: any) => {
                resolve((response.data && response.data.models) || []);
            })
            .catch((error: any) => {
                console.error('Failed to list Ollama models:', error);
                resolve([]);
            });
    });
}

// 检查Ollama模型状态
export function checkOllamaModels(models: string[]): Promise<{ models: Record<string, boolean> }> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/ollama/models/check', { models })
            .then((response: any) => {
                resolve(response.data || { models: {} });
            })
            .catch((error: any) => {
                console.error('Failed to check Ollama models:', error);
                reject(error);
            });
    });
}

// 启动Ollama模型下载（异步）
export function downloadOllamaModel(modelName: string): Promise<{ taskId: string; modelName: string; status: string; progress: number }> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/ollama/models/download', { modelName })
            .then((response: any) => {
                resolve(response.data || { taskId: '', modelName, status: 'failed', progress: 0 });
            })
            .catch((error: any) => {
                console.error('Failed to start Ollama model download:', error);
                reject(error);
            });
    });
}

// 查询下载进度
export function getDownloadProgress(taskId: string): Promise<DownloadTask> {
    return new Promise((resolve, reject) => {
        get(`/api/v1/initialization/ollama/download/progress/${taskId}`)
            .then((response: any) => {
                resolve(response.data);
            })
            .catch((error: any) => {
                console.error('Failed to get download progress:', error);
                reject(error);
            });
    });
}

// 获取所有下载任务
export function listDownloadTasks(): Promise<DownloadTask[]> {
    return new Promise((resolve, reject) => {
        get('/api/v1/initialization/ollama/download/tasks')
            .then((response: any) => {
                resolve(response.data || []);
            })
            .catch((error: any) => {
                console.error('Failed to list download tasks:', error);
                reject(error);
            });
    });
}

export function getCurrentConfigByKB(kbId: string): Promise<InitializationConfig & { hasFiles: boolean }> {
    return new Promise((resolve, reject) => {
        get(`/api/v1/initialization/config/${kbId}`)
            .then((response: any) => {
                resolve(response.data || {});
            })
            .catch((error: any) => {
                console.error('Failed to get KB config:', error);
                reject(error);
            });
    });
}

// 所有"测试连接"接口共用的通用可选参数。
// customHeaders / extraConfig / interfaceType 对应后端 ModelTestRequest 里的同名字段，
// 会被透传给真正的模型装配流程，保证测试连接与生产调用走完全相同的路径。
interface BaseModelTestPayload {
    customHeaders?: Record<string, string>;
    extraConfig?: Record<string, string>;
    interfaceType?: string;
}

// 检查远程API模型
export function checkRemoteModel(modelConfig: {
    modelName: string;
    baseUrl: string;
    apiKey?: string;
    provider?: string;
    // 编辑已存在模型时传 modelId，后端会自动从存储中带出 apiKey
    // （前端不再回显明文密钥，所以测试连接必须用这个回填路径）
    modelId?: string;
} & BaseModelTestPayload): Promise<{
    available: boolean;
    message?: string;
}> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/remote/check', modelConfig)
            .then((response: any) => {
                resolve(response.data || {});
            })
            .catch((error: any) => {
                console.error('Failed to check remote model:', error);
                reject(error);
            });
    });
}

// 测试 Embedding 模型（本地/远程）是否可用
export function testEmbeddingModel(modelConfig: {
    source: 'local' | 'remote';
    modelName: string;
    baseUrl?: string;
    apiKey?: string;
    dimension?: number;
    supportsDimensionOverride?: boolean;
    provider?: string;
    modelId?: string;
} & BaseModelTestPayload): Promise<{ available: boolean; message?: string; dimension?: number }> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/embedding/test', modelConfig)
            .then((response: any) => {
                resolve(response.data || {});
            })
            .catch((error: any) => {
                console.error('Failed to test Embedding model:', error);
                reject(error);
            });
    });
}

export function checkRerankModel(modelConfig: {
    modelName: string;
    baseUrl: string;
    apiKey?: string;
    provider?: string;
    modelId?: string;
} & BaseModelTestPayload): Promise<{
    available: boolean;
    message?: string;
}> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/rerank/check', modelConfig)
            .then((response: any) => {
                resolve(response.data || {});
            })
            .catch((error: any) => {
                console.error('Failed to check Rerank model:', error);
                reject(error);
            });
    });
}

export function testMultimodalFunction(testData: {
    image: File;
    vlm_model: string;
    vlm_base_url: string;
    vlm_api_key?: string;
    vlm_interface_type?: string;
    chunk_size: number;
    chunk_overlap: number;
    separators: string[];
}): Promise<{
    success: boolean;
    caption?: string;
    ocr?: string;
    processing_time?: number;
    message?: string;
}> {
    return new Promise((resolve, reject) => {
        const formData = new FormData();
        formData.append('image', testData.image);
        formData.append('vlm_model', testData.vlm_model);
        formData.append('vlm_base_url', testData.vlm_base_url);
        if (testData.vlm_api_key) {
            formData.append('vlm_api_key', testData.vlm_api_key);
        }
        if (testData.vlm_interface_type) {
            formData.append('vlm_interface_type', testData.vlm_interface_type);
        }
        formData.append('chunk_size', testData.chunk_size.toString());
        formData.append('chunk_overlap', testData.chunk_overlap.toString());
        formData.append('separators', JSON.stringify(testData.separators));

        // 获取鉴权Token
        const token = localStorage.getItem('reliquary_token');
        const headers: Record<string, string> = {};
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }


        // 使用原生fetch因为需要发送FormData
        fetch('/api/v1/initialization/multimodal/test', {
            method: 'POST',
            headers,
            body: formData
        })
            .then(response => response.json())
            .then((data: any) => {
                if (data.success) {
                    resolve(data.data || {});
                } else {
                    resolve({ success: false, message: data.message || t('error.initialization.testFailed') });
                }
            })
            .catch((error: any) => {
                console.error('Failed multimodal test:', error);
                reject(error);
            });
    });
}

export interface FabriTextRequest {
    tags: string[];
    model_id: string;
}

export interface FabriTextResponse {
    text: string;
}

// 文本内容生成
export function fabriText(request: FabriTextRequest): Promise<FabriTextResponse> {
    return new Promise((resolve, reject) => {
        post('/api/v1/initialization/extract/fabri-text', request)
            .then((response: any) => {
                resolve(response.data || { text: '' });
            })
            .catch((error: any) => {
                console.error('Failed to generate text:', error);
                reject(error);
            });
    });
}

// 模型厂商信息类型
export interface ModelProviderOption {
    value: string;        // provider 标识符
    label: string;        // 显示名称
    description: string;  // 描述
    defaultUrls: Record<string, string>;  // 按模型类型区分的默认 URL
    modelTypes: string[]; // 支持的模型类型
}

// 获取模型厂商列表
export function listModelProviders(modelType?: string): Promise<ModelProviderOption[]> {
    return new Promise((resolve, reject) => {
        const url = modelType
            ? `/api/v1/models/providers?model_type=${encodeURIComponent(modelType)}`
            : '/api/v1/models/providers';
        get(url)
            .then((response: any) => {
                resolve(response.data || []);
            })
            .catch((error: any) => {
                console.error('Failed to list model providers:', error);
                resolve([]); // 失败时返回空数组，不生成另一份预设数据
            });
    });
}
