from reliquary_mcp_server import ReliquaryClient, _normalize_kb_entries

# Shapes mirror the team knowledge-base list API.


def test_normalize_kb_entries_passes_through_team_rows():
    resp = {
        "success": True,
        "data": [{"id": "kb-team-1", "name": "Team Docs"}],
    }
    assert _normalize_kb_entries(resp) == [{"id": "kb-team-1", "name": "Team Docs"}]


def test_resolve_kb_id_accepts_team_kb_name(monkeypatch):
    client = ReliquaryClient("http://example.test/api/v1", "")

    def fake_request(method, path, **kwargs):
        assert method == "GET" and path == "/knowledge-bases"
        return {"success": True, "data": [{"id": "kb-team-1", "name": "技术文档库"}]}

    monkeypatch.setattr(client, "_request", fake_request)
    assert client.resolve_kb_id("技术文档库") == "kb-team-1"
