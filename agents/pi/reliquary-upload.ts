import { openAsBlob } from "node:fs";
import { realpath, stat } from "node:fs/promises";
import { basename, isAbsolute, relative, resolve, sep } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

interface UploadOptions {
  workspaceRoot?: string;
  apiUrl?: string;
}

function within(root: string, path: string) {
  const remainder = relative(root, path);
  return remainder !== ".." && !remainder.startsWith(`..${sep}`) && !isAbsolute(remainder);
}

async function workspaceFile(workspaceRoot: string, input: string) {
  const path = input.startsWith("@") ? input.slice(1) : input;
  if (!path || path.includes("\0") || path.split(/[\\/]/).includes("..")) {
    throw new Error("文件路径不能为空、包含空字符或使用 .. 路径段");
  }
  const root = resolve(workspaceRoot);
  const candidate = resolve(root, path);
  if (!within(root, candidate)) throw new Error("只能上传工作文件夹内的文件");
  const canonicalRoot = await realpath(root);
  const canonicalFile = await realpath(candidate);
  if (!within(canonicalRoot, canonicalFile)) throw new Error("符号链接指向工作文件夹之外，不能上传");
  if (!(await stat(canonicalFile)).isFile()) throw new Error("请选择工作文件夹内的普通文件，不能上传目录");
  return { path: canonicalFile, name: basename(candidate) };
}

// Pi supplies only pi; alternate roots and API URLs let tests use real local files and HTTP.
export default function uploadExtension(pi: ExtensionAPI, options: UploadOptions = {}) {
  const workspaceRoot = options.workspaceRoot ?? "/workspace";
  const apiUrl = options.apiUrl ?? "http://app:8080";

  async function upload(libraryID: string, route: string, path: string, fields: Record<string, string>, signal?: AbortSignal) {
    signal?.throwIfAborted();
    const key = process.env.RELIQUARY_API_KEY;
    if (!key) throw new Error("未配置 RELIQUARY_API_KEY，无法上传文件");
    const file = await workspaceFile(workspaceRoot, path);
    const form = new FormData();
    form.append("file", await openAsBlob(file.path), file.name);
    for (const [name, value] of Object.entries(fields)) form.append(name, value);
    const response = await fetch(`${apiUrl}/api/v1/libraries/${encodeURIComponent(libraryID)}/${route}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${key}` },
      body: form,
      signal,
      redirect: "manual",
    });
    // Preserve the response body, including non-JSON errors and whitespace.
    return {
      content: [{ type: "text" as const, text: await response.text() }],
      details: { status: response.status },
      isError: !response.ok,
    };
  }

  const libraryID = Type.String({ minLength: 1, description: "目标货架库 ID" });
  const path = Type.String({ minLength: 1, description: "工作文件夹 /workspace 内的相对路径或绝对路径" });
  pi.registerTool({
    name: "reliquary_upload_workbook",
    label: "上传工作簿",
    description: "上传工作文件夹中的 Excel 工作簿。提供已确认的 plan_key 时按方案生成待确认导入变更集；没有方案时省略 plan_key，提供 new_plan_key 和 file_kind 请求待审核方案建议。原样返回后端结果（含变更集 ID），不确认变更。",
    promptSnippet: "上传工作簿，生成待确认导入变更集或方案建议",
    parameters: Type.Object({
      library_id: libraryID,
      path,
      plan_key: Type.Optional(Type.String({ minLength: 1, description: "本库已有且已确认的导入方案键；没有方案时省略" })),
      file_date: Type.Optional(Type.String({ minLength: 1, description: "可选 RFC3339 文件生效时间；文件名日期不可靠时必填，仅用于导入，不用于方案建议" })),
      new_plan_key: Type.Optional(Type.String({ minLength: 1, description: "没有方案时必填：建议创建的新方案键，作为建议接口的 plan_key" })),
      file_kind: Type.Optional(Type.String({ minLength: 1, description: "没有方案时必填：product_map、certification_progress、certification_report、overseas_sales 或 atq" })),
    }),
    async execute(_toolCallID, params, signal) {
      if (params.plan_key) {
        const fields: Record<string, string> = { plan_key: params.plan_key };
        if (params.file_date !== undefined) fields.file_date = params.file_date;
        return upload(params.library_id, "shelf/imports", params.path, fields, signal);
      }
      if (!params.new_plan_key?.trim() || !params.file_kind?.trim()) {
        throw new Error("没有已确认方案时，必须提供 new_plan_key 和 file_kind 以请求待审核方案建议");
      }
      return upload(params.library_id, "shelf/import-plans/suggest", params.path,
        { plan_key: params.new_plan_key, file_kind: params.file_kind }, signal);
    },
  });
  pi.registerTool({
    name: "reliquary_stage_attachment",
    label: "暂存原件",
    description: "把工作文件夹内的原件暂存为目标库的资料附件，原样返回后端结果（data.id 为附件 ID），供 MCP 的 add_document_version 使用。只暂存，不确认货架关联；单文件上限由后端按库配置检查。",
    promptSnippet: "暂存原件，返回用于 add_document_version 的附件 ID",
    parameters: Type.Object({ library_id: libraryID, path }),
    async execute(_toolCallID, params, signal) {
      return upload(params.library_id, "entity-attachments", params.path, {}, signal);
    },
  });
}
