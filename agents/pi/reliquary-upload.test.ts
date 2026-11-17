// Ticket 02, medium/spec: a local HTTP server replaces the Reliquary upload API.
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, mkdir, rm, symlink, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import type { ExtensionAPI, ExtensionToolContext, ToolDefinition } from "@earendil-works/pi-coding-agent";
import uploadExtension from "./reliquary-upload.ts";

const workbook = "reliquary_upload_workbook";
const attachment = "reliquary_stage_attachment";
const bytes = Buffer.from([0x50, 0x4b, 0, 0xff, 0x0a]);
const pending = '{"success":true,"data":{"id":"pending-import","status":"pending"}}';

async function fixture(t: TestContext, useHttp = true) {
  const root = await mkdtemp(join(import.meta.dirname, ".upload-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = join(root, "workspace");
  const sibling = join(root, "workspace-other");
  await mkdir(join(workspace, "表格"), { recursive: true });
  await mkdir(sibling);
  await writeFile(join(workspace, "表格", "产品地图 2026-11-01.xlsx"), bytes);
  await writeFile(join(workspace, "原件.pdf"), bytes);
  await writeFile(join(sibling, "secret.xlsx"), "outside");
  await symlink(join(sibling, "secret.xlsx"), join(workspace, "escape.xlsx"));
  await symlink(sibling, join(workspace, "escape-dir"));
  await symlink(join(workspace, "原件.pdf"), join(workspace, "local.pdf"));
  await symlink(join(workspace, "表格"), join(workspace, "local-dir"));

  const requests: { method: string | undefined; url: string | undefined; authorization: string | undefined; form: FormData }[] = [];
  let reply = { status: 201, body: pending };
  let apiUrl = "http://127.0.0.1:1";
  if (useHttp) {
    const server = createServer(async (request, response) => {
      const chunks = [];
      for await (const chunk of request) chunks.push(chunk);
      const form = await new Response(Buffer.concat(chunks), {
        headers: { "Content-Type": request.headers["content-type"]! },
      }).formData();
      requests.push({ method: request.method, url: request.url, authorization: request.headers.authorization, form });
      response.writeHead(reply.status);
      response.end(reply.body);
    });
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    t.after(() => new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())));
    const address = server.address();
    assert.ok(address && typeof address !== "string");
    apiUrl = `http://127.0.0.1:${address.port}`;
  }
  // Rejected inputs must never reach fetch, even when a listener is unavailable.
  const unexpectedFetch = useHttp ? undefined : t.mock.method(globalThis, "fetch", async () => {
    throw new Error("被拒绝的上传不应发送请求");
  });
  const savedKey = process.env.RELIQUARY_API_KEY;
  process.env.RELIQUARY_API_KEY = "synthetic-pi-key";
  t.after(() => {
    if (savedKey === undefined) delete process.env.RELIQUARY_API_KEY;
    else process.env.RELIQUARY_API_KEY = savedKey;
  });
  const tools = new Map<string, ToolDefinition>();
  uploadExtension({ registerTool: tool => tools.set(tool.name, tool) } as ExtensionAPI,
    { workspaceRoot: workspace, apiUrl });

  return {
    workspace, sibling, requests, tools,
    assertNoRequests() {
      assert.equal(requests.length, 0);
      assert.equal(unexpectedFetch?.mock.callCount() ?? 0, 0);
    },
    reply(status: number, body: string) { reply = { status, body }; },
    call(name: string, params: Record<string, unknown>) {
      const tool = tools.get(name);
      assert.ok(tool, `Missing upload tool: ${name}`);
      return tool.execute("upload-call", { library_id: "library-one", ...params }, undefined, undefined, {} as ExtensionToolContext);
    },
  };
}

test("上传工作簿携带已确认方案、文件日期、原始文件和 Bearer 认证，返回待确认变更集", async t => {
  const f = await fixture(t);
  const result = await f.call(workbook, {
    path: "表格/产品地图 2026-11-01.xlsx", plan_key: "product_map", file_date: "2026-11-01T00:00:00Z",
  });
  assert.equal(result.isError, false);
  assert.deepEqual(result.content, [{ type: "text", text: pending }]);
  assert.equal(f.requests.length, 1);
  const request = f.requests[0];
  assert.equal(request.method, "POST");
  assert.equal(request.url, "/api/v1/libraries/library-one/shelf/imports");
  assert.equal(request.authorization, "Bearer synthetic-pi-key");
  assert.deepEqual([...request.form.keys()].sort(), ["file", "file_date", "plan_key"]);
  assert.equal(request.form.get("plan_key"), "product_map");
  assert.equal(request.form.get("file_date"), "2026-11-01T00:00:00Z");
  const file = request.form.get("file") as File;
  assert.equal(file.name, "产品地图 2026-11-01.xlsx");
  assert.deepEqual(Buffer.from(await file.arrayBuffer()), bytes);
});

test("上传可使用工作文件夹内的绝对路径，未提供 file_date 时交由后端识别文件名日期", async t => {
  const f = await fixture(t);
  await f.call(workbook, { path: join(f.workspace, "表格/产品地图 2026-11-01.xlsx"), plan_key: "product_map" });
  assert.deepEqual([...f.requests[0].form.keys()].sort(), ["file", "plan_key"]);
});

test("没有已确认方案时用新方案键和文件种类请求待审核方案，不发送文件日期或导入事实", async t => {
  const f = await fixture(t);
  const suggestion = '{"success":true,"data":{"id":"pending-plan","operations":[{"type":"save_import_plan"}]}}';
  f.reply(201, suggestion);
  const result = await f.call(workbook, {
    path: "表格/产品地图 2026-11-01.xlsx", new_plan_key: "customer-map", file_kind: "product_map",
    file_date: "2026-11-01T00:00:00Z",
  });
  assert.equal(result.isError, false);
  assert.deepEqual(result.content, [{ type: "text", text: suggestion }]);
  assert.equal(f.requests.length, 1);
  const request = f.requests[0];
  assert.equal(request.method, "POST");
  assert.equal(request.url, "/api/v1/libraries/library-one/shelf/import-plans/suggest");
  assert.equal(request.authorization, "Bearer synthetic-pi-key");
  assert.deepEqual([...request.form.keys()].sort(), ["file", "file_kind", "plan_key"]);
  assert.equal(request.form.get("plan_key"), "customer-map");
  assert.equal(request.form.get("file_kind"), "product_map");
  const file = request.form.get("file") as File;
  assert.equal(file.name, "产品地图 2026-11-01.xlsx");
  assert.deepEqual(Buffer.from(await file.arrayBuffer()), bytes);
});

test("暂存原件只上传文件并返回附件 ID，供 add_document_version 提议使用", async t => {
  const f = await fixture(t);
  const staged = '{"success":true,"data":{"id":"staged-attachment","max_file_bytes":4096}}';
  f.reply(201, staged);
  const result = await f.call(attachment, { path: "原件.pdf" });
  assert.equal(result.isError, false);
  assert.deepEqual(result.content, [{ type: "text", text: staged }]);
  assert.equal(f.requests.length, 1);
  const request = f.requests[0];
  assert.equal(request.method, "POST");
  assert.equal(request.url, "/api/v1/libraries/library-one/entity-attachments");
  assert.equal(request.authorization, "Bearer synthetic-pi-key");
  assert.deepEqual([...request.form.keys()], ["file"]);
  const file = request.form.get("file") as File;
  assert.equal(file.name, "原件.pdf");
  assert.deepEqual(Buffer.from(await file.arrayBuffer()), bytes);
});

test("两个上传工具均拒绝工作文件夹外路径、任何 .. 路径段和符号链接逃逸，且不发送请求", async t => {
  const f = await fixture(t, false);
  for (const name of [workbook, attachment]) {
    for (const path of [join(f.sibling, "secret.xlsx"), "/data/agent/auth.json", "../workspace-other/secret.xlsx",
      "表格/../原件.pdf", `${f.workspace}/../workspace/原件.pdf`, "..\\workspace-other\\secret.xlsx",
      "escape.xlsx", "escape-dir/secret.xlsx"]) {
      await assert.rejects(f.call(name, { path, plan_key: "product_map" }), /工作文件夹|\.\./, `${name}: ${path}`);
    }
  }
  f.assertNoRequests();
});

test("两个上传工具接受仍位于工作文件夹内的符号链接及 Pi 的 @ 路径前缀", async t => {
  const f = await fixture(t);
  await f.call(workbook, { path: "@local-dir/产品地图 2026-11-01.xlsx", plan_key: "product_map" });
  await f.call(attachment, { path: "local.pdf" });
  for (const request of f.requests) {
    assert.deepEqual(Buffer.from(await (request.form.get("file") as File).arrayBuffer()), bytes);
  }
  assert.equal(f.requests.length, 2);
});

test("两个上传工具拒绝目录、空路径和不存在的文件", async t => {
  const f = await fixture(t, false);
  for (const name of [workbook, attachment]) {
    for (const path of ["", ".", "表格", "missing.xlsx"]) {
      await assert.rejects(f.call(name, { path, plan_key: "product_map" }));
    }
  }
  f.assertNoRequests();
});

test("方案建议缺少新方案键或文件种类时不猜测配置、不发送请求", async t => {
  const f = await fixture(t, false);
  for (const params of [{}, { file_kind: "product_map" }, { new_plan_key: "customer-map" }]) {
    await assert.rejects(f.call(workbook, { path: "原件.pdf", ...params }), /new_plan_key|file_kind/);
  }
  f.assertNoRequests();
});

for (const [name, params] of [
  [workbook, { plan_key: "product_map" }],
  [workbook, { new_plan_key: "customer-map", file_kind: "product_map" }],
  [attachment, {}],
] as const) {
  test(`${name} ${JSON.stringify(params)} 将后端 JSON、大小上限和非 JSON 错误原样作为工具失败返回`, async t => {
    const f = await fixture(t);
    for (const [status, body] of [
      [400, ' {"success":false,"error":{"message":"configured size limit exceeded","max_file_bytes":4}}\n'],
      [401, '{"success":false,"error":"unauthorized"}'],
      [409, '{"success":false,"error":"plan already exists"}'],
      [413, "上传文件超过库的单文件上限\n"],
      [502, "upstream unavailable\n"],
    ] as const) {
      f.reply(status, body);
      const result = await f.call(name, { path: "原件.pdf", ...params });
      assert.equal(result.isError, true);
      assert.deepEqual(result.content, [{ type: "text", text: body }]);
      assert.equal((result.details as { status: number }).status, status);
    }
  });
}

test("没有 RELIQUARY_API_KEY 时两个上传工具均失败且不发送请求", async t => {
  const f = await fixture(t, false);
  delete process.env.RELIQUARY_API_KEY;
  for (const name of [workbook, attachment]) {
    await assert.rejects(f.call(name, { path: "原件.pdf", plan_key: "product_map" }), /RELIQUARY_API_KEY/);
  }
  f.assertNoRequests();
});
