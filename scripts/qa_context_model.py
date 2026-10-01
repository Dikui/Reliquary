#!/usr/bin/env python3
"""Local QA fixture for the two claude.dev acceptance articles, not a real LLM.

Run --check, then point the QA summary model at http://127.0.0.1:18183/v1.
Other chat tasks are forwarded to the existing intake fixture on port 18083.
"""

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener


def context_reply(messages):
    prompt = next(m["content"] for m in messages if m["role"] == "system")
    if "source mention is the exact same real-world object" in prompt:
        return {"entity_id": "", "confidence": 0}
    if "same specific real-world change" in prompt:
        return {"event_id": "", "confidence": 0}
    if not prompt.startswith("Extract only source-supported objects"):
        return None
    source = json.loads(next(m["content"] for m in messages if m["role"] == "user"))
    # ponytail: two fixed acceptance cases; use a real model for semantic quality.
    cases = [
        ("Claude Sonnet 5.5", "model", "Claude Sonnet 5.5 upgrade",
         "QA 固定样例：原文介绍 Sonnet 5.5 的模型升级、速度和成本变化，值得评估迁移。"),
        ("claude-api skill", "tool", "claude-api skill adds evaluation guidance",
         "QA 固定样例：原文新增评估设计与性能改进指南，可用于改进应用或技能的评估流程。"),
    ]
    for mention, kind, title, reason in cases:
        quote = next((p.strip() for p in source["body"].split("\n\n") if mention in p), "")
        if quote:
            return {
                "entities": [{"mention": mention, "quote": quote, "canonical_name": mention,
                              "entity_type": kind, "description": mention, "aliases": []}],
                "facts": [{"kind": "attribute", "subject": mention, "predicate": "source_description",
                           "value": quote, "value_type": "string", "quote": quote}],
                "events": [{"title": title, "event_type": "update", "description": quote,
                            "entity_mentions": [mention], "quote": quote}],
                "attention": {**dict.fromkeys(("novelty", "impact", "credibility", "relevance",
                                               "actionability", "score"), 50), "reason": reason},
            }
    raise ValueError("context fixture supports only the two claude.dev acceptance articles")


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        try:
            size = int(self.headers.get("Content-Length", 0))
            if not 0 < size <= 1_000_000:
                raise ValueError("invalid request size")
            raw = self.rfile.read(size)
            request = json.loads(raw)
            reply = context_reply(request["messages"])
            if reply is None:
                headers = {"Content-Type": "application/json"}
                if self.headers.get("Authorization"):
                    headers["Authorization"] = self.headers["Authorization"]
                forwarded = Request(self.server.upstream + self.path, data=raw, headers=headers)
                with build_opener(ProxyHandler({})).open(forwarded, timeout=30) as response:
                    data, content_type = response.read(), response.headers.get("Content-Type", "application/json")
            else:
                data = json.dumps({"id": "qa-context", "object": "chat.completion",
                                   "model": request.get("model", "qa-context"),
                                   "choices": [{"index": 0, "finish_reason": "stop", "message": {
                                       "role": "assistant", "content": json.dumps(reply, ensure_ascii=False)}}]}).encode()
                content_type = "application/json"
        except (ValueError, KeyError, StopIteration) as error:
            self.send_error(400, str(error))
            return
        except (HTTPError, URLError, TimeoutError):
            self.send_error(502, "upstream QA fixture failed")
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def check():
    assert context_reply([{"role": "system", "content": "Decide whether the source mention is the exact same real-world object as one candidate"}]) == {"entity_id": "", "confidence": 0}
    assert context_reply([{"role": "system", "content": "Determine whether new_event describes the same specific real-world change"}]) == {"event_id": "", "confidence": 0}
    for body in ("Claude Sonnet 5.5 is an upgrade.", "Guidance was added to claude-api skill."):
        reply = context_reply([{"role": "system", "content": "Extract only source-supported objects"},
                               {"role": "user", "content": json.dumps({"body": body})}])
        assert reply["attention"]["reason"].strip()
        for field in ("entities", "facts", "events"):
            assert len(reply[field]) == 1 and reply[field][0]["quote"] in body
        assert context_reply([{"role": "system", "content": "Screen the article"}]) is None
    print("QA context schema check passed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=18183)
    parser.add_argument("--upstream", default="http://127.0.0.1:18083")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if args.check:
        check()
    else:
        server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
        server.upstream = args.upstream.rstrip("/")
        server.serve_forever()
