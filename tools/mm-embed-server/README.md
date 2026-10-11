# 可选的本地多模态 embedding 服务

使用 transformers 在 Apple Silicon 的 MPS 或 CPU 上运行共享文字与图片向量空间的模型。默认模型为 `google/siglip2-base-patch16-224`，Apache 2.0 许可，完整双塔约 375M 参数，768 维，float32。这是链路验证用模型，未进行模型对比评测。

从仓库根目录执行，Python 3.11 到 3.13：

```sh
python3 -m venv .scratch/kb-reshape/evidence/12/.venv
.scratch/kb-reshape/evidence/12/.venv/bin/python -m pip install -r tools/mm-embed-server/requirements.txt
HF_HOME="$HOME/.cache/reliquary-mm-models" \
  .scratch/kb-reshape/evidence/12/.venv/bin/python tools/mm-embed-server/server.py \
  --model google/siglip2-base-patch16-224 --device auto --port 18098
```

默认仅监听 localhost。`--device cpu` 可在没有 MPS 的环境运行。每次只执行一个模型推理，避免并发请求提高内存峰值。`GET /health` 返回模型、参数量、维度、设备及进程峰值 RSS，MPS 时额外返回 Metal 已分配内存。macOS 的 RSS 单位为字节。

接口采用 [Jina 多模态 embedding](https://jina.ai/en-US/embeddings/) 的 `input` 对象和 OpenAI 风格的 `data[index].embedding` 响应：

```sh
curl http://127.0.0.1:18098/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"google/siglip2-base-patch16-224","input":[{"text":"一辆红色自行车"}],"encoding_format":"float"}'
```

图片使用 `{"image":"data:image/png;base64,..."}`，同一请求可混合文字和图片。也接受原有文字字符串或字符串数组。图片只接受内联 data URI，不下载远程 URL。单图最多 8 MiB、2500 万像素，每批最多 16 项，请求体最多 32 MiB。所有向量 L2 归一化，不支持改维度或 base64 输出。错误请求返回 400，超长请求体返回 413，推理失败返回 500。

SigLIP 2 原生文字上下文只有 64 tokens。长文字按完整 token 窗口依次编码，平均归一化窗口向量后再次归一化，避免只保留文章前缀。最多 256 个窗口，超出返回错误。这个聚合方式不能替代针对长文训练的 embedding 模型，实际检索质量须在个人语料中观察。

`--model` 支持具有 `get_text_features` 和 `get_image_features` 的 transformers 双塔模型，例如其他 SigLIP、SigLIP 2、CLIP 型号。`--revision` 可固定官方模型版本。服务不执行模型仓库中的 remote code。部署时采用其他架构的大模型，可换用支持该架构的 Jina 格式 HTTP 服务，Reliquary 侧接口保持相同；本服务不声称支持任意多模态模型。模型变更后同步修改 Reliquary 模型名称、维度和支持图片标记，然后重建全部向量。详见 [个人部署文档](../../docs/personal-use.md)。

权重应保存在仓库外的 `HF_HOME` 中。虚拟环境放任务目录且不提交。Ctrl+C 停止服务。

无需权重的接口校验测试：

```sh
python3 -m unittest discover -s tools/mm-embed-server -p 'test_*.py'
```
