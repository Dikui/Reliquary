"""Optional local text/image embedding service using the Jina input shape."""

import argparse
import base64
import binascii
import io
import json
import resource
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_BODY = 32 * 1024 * 1024
MAX_IMAGE = 8 * 1024 * 1024


def parse_inputs(body, model_name, dimension):
    if not isinstance(body, dict):
        raise ValueError("request must be an object")
    if body.get("model") != model_name:
        raise ValueError("model does not match the loaded model")
    if body.get("encoding_format", "float") != "float":
        raise ValueError("only float encoding is supported")
    if body.get("dimensions", dimension) != dimension:
        raise ValueError("this model does not support dimension overrides")
    inputs = body.get("input")
    if isinstance(inputs, str):
        inputs = [inputs]
    if not isinstance(inputs, list) or not 1 <= len(inputs) <= 16:
        raise ValueError("input must contain 1 to 16 items")
    parsed = []
    for item in inputs:
        if isinstance(item, str):
            item = {"text": item}
        if not isinstance(item, dict) or len(item) != 1:
            raise ValueError("each input must contain exactly one text or image")
        if "text" in item:
            value = item["text"]
            if not isinstance(value, str) or not value.strip() or len(value) > 32768:
                raise ValueError("text must contain 1 to 32768 characters")
            parsed.append(("text", value))
        elif "image" in item:
            value = item["image"]
            if not isinstance(value, str) or not value.startswith("data:image/"):
                raise ValueError("image must be a base64 data URI; remote URLs are unsupported")
            header, separator, payload = value.partition(",")
            if not separator or not header.endswith(";base64") or len(payload) > (MAX_IMAGE + 2) // 3 * 4:
                raise ValueError("invalid image data URI or image exceeds 8 MiB")
            try:
                decoded = base64.b64decode(payload, validate=True)
            except (ValueError, binascii.Error) as exc:
                raise ValueError("invalid image base64") from exc
            if not decoded or len(decoded) > MAX_IMAGE:
                raise ValueError("image must contain 1 byte to 8 MiB")
            parsed.append(("image", decoded))
        else:
            raise ValueError("unsupported input modality")
    return parsed


class Encoder:
    def __init__(self, name, device="auto", revision=None):
        import torch
        from transformers import AutoModel, AutoProcessor

        self.torch = torch
        self.name = name
        self.device = "mps" if device == "auto" and torch.backends.mps.is_available() else device
        if self.device == "auto":
            self.device = "cpu"
        torch.set_num_threads(2)
        self.model = AutoModel.from_pretrained(name, revision=revision, trust_remote_code=False).to(self.device).eval()
        self.processor = AutoProcessor.from_pretrained(name, revision=revision, trust_remote_code=False)
        if not all(hasattr(self.model, method) for method in ("get_text_features", "get_image_features")):
            raise ValueError("model must implement get_text_features and get_image_features")
        text = self.model.config.text_config
        self.max_length = text.max_position_embeddings
        self.dimension = getattr(text, "projection_size", None) or getattr(self.model.config, "projection_dim", None) or text.hidden_size
        self.parameters = sum(p.numel() for p in self.model.parameters())
        self.lock = threading.Lock()

    def encode(self, inputs):
        from PIL import Image, UnidentifiedImageError

        torch = self.torch
        vectors = []
        tokens = 0
        with self.lock, torch.inference_mode():
            for modality, value in inputs:
                if modality == "text":
                    ids = self.processor.tokenizer(value, add_special_tokens=False)["input_ids"]
                    width = self.max_length - self.processor.tokenizer.num_special_tokens_to_add()
                    if len(ids) > width * 256:
                        raise ValueError("text exceeds 256 model context windows")
                    windows = [ids[i:i + width] for i in range(0, len(ids), width)] or [[]]
                    features = []
                    for window in windows:
                        text = self.processor.tokenizer.decode(window)
                        batch = self.processor(text=[text], padding="max_length", max_length=self.max_length,
                                               truncation=True, return_tensors="pt").to(self.device)
                        feature = self.model.get_text_features(**batch)
                        features.append(torch.nn.functional.normalize(feature, dim=-1))
                    vector = torch.stack(features).mean(dim=0)
                    tokens += len(ids)
                else:
                    try:
                        with Image.open(io.BytesIO(value)) as source:
                            if source.width * source.height > 25_000_000:
                                raise ValueError("image exceeds 25 million pixels")
                            image = source.convert("RGB")
                    except (UnidentifiedImageError, OSError, Image.DecompressionBombError) as exc:
                        raise ValueError("invalid or oversized image") from exc
                    batch = self.processor(images=[image], return_tensors="pt").to(self.device)
                    vector = self.model.get_image_features(**batch)
                vector = torch.nn.functional.normalize(vector, dim=-1).float().cpu()[0]
                if vector.numel() != self.dimension or not torch.isfinite(vector).all() or vector.norm() == 0:
                    raise RuntimeError("model returned an invalid vector")
                vectors.append(vector.tolist())
        return vectors, tokens

    def metadata(self):
        memory = {"rss_peak_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss}
        if self.device == "mps":
            memory.update(mps_allocated_bytes=self.torch.mps.current_allocated_memory(),
                          mps_driver_bytes=self.torch.mps.driver_allocated_memory())
        return {"model": self.name, "dimension": self.dimension, "parameters": self.parameters,
                "device": self.device, "text_context": self.max_length, **memory}


class Handler(BaseHTTPRequestHandler):
    def respond(self, status, body):
        encoded = json.dumps(body, allow_nan=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self):
        if self.path == "/health":
            self.respond(200, self.server.encoder.metadata())
        else:
            self.respond(404, {"error": "unknown route"})

    def do_POST(self):
        if self.path != "/v1/embeddings":
            self.respond(404, {"error": "unknown route"})
            return
        start = time.perf_counter()
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 1 <= size <= MAX_BODY:
                self.respond(413, {"error": "request must contain 1 byte to 32 MiB"})
                return
            body = json.loads(self.rfile.read(size))
            encoder = self.server.encoder
            inputs = parse_inputs(body, encoder.name, encoder.dimension)
            vectors, tokens = encoder.encode(inputs)
            elapsed = (time.perf_counter() - start) * 1000
            self.respond(200, {"object": "list", "model": encoder.name,
                               "data": [{"object": "embedding", "index": i, "embedding": v}
                                        for i, v in enumerate(vectors)],
                               "usage": {"prompt_tokens": tokens, "total_tokens": tokens},
                               "elapsed_ms": elapsed})
            print(json.dumps({"modalities": [m for m, _ in inputs], "elapsed_ms": elapsed,
                              **encoder.metadata()}), flush=True)
        except (ValueError, UnicodeError) as exc:
            self.respond(400, {"error": str(exc)})
        except Exception:
            self.log_error("embedding inference failed")
            self.respond(500, {"error": "embedding inference failed; see server logs"})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", default="google/siglip2-base-patch16-224")
    parser.add_argument("--revision", help="pin a Hugging Face commit or local model revision")
    parser.add_argument("--device", choices=["auto", "cpu", "mps"], default="auto")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=18098)
    args = parser.parse_args()
    encoder = Encoder(args.model, args.device, args.revision)
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    server.encoder = encoder
    print(json.dumps({"ready": True, **encoder.metadata()}), flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
