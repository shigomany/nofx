from __future__ import annotations

from typing import Any
from uuid import uuid4

from fastapi.testclient import TestClient

from apps.codex_gateway.app_server import CodexProtocolError
from apps.codex_gateway.main import _load_service_token, create_app


class Profile:
    async def models(self) -> dict[str, Any]:
        return {"data": [{"id": "gpt-6.1-sol"}], "nextCursor": None}

    async def generate(self, **_: Any) -> Any:
        return type(
            "Result",
            (),
            {
                "content": {"response": "free text"},
                "model": "gpt-5.6-luna",
                "effort": "low",
                "usage": {},
                "elapsed_ms": 10,
            },
        )()


class Manager:
    def __init__(self, profile: Any | None = None) -> None:
        self.value = profile or Profile()

    def profile(self, _: str) -> Any:
        return self.value


TOKEN = "service-token-with-at-least-32-characters"


def test_private_bearer_required_and_short_configuration_fails_closed() -> None:
    profile_id = str(uuid4())
    good = TestClient(create_app(manager=Manager(), service_token=TOKEN))
    assert good.get(f"/v1/profiles/{profile_id}/models").status_code == 401
    assert (
        good.get(
            f"/v1/profiles/{profile_id}/models",
            headers={"Authorization": "Bearer wrong"},
        ).status_code
        == 401
    )
    assert (
        good.get(
            f"/v1/profiles/{profile_id}/models",
            headers={"Authorization": f"Bearer {TOKEN}"},
        ).status_code
        == 200
    )
    short = TestClient(create_app(manager=Manager(), service_token="short"))
    assert (
        short.get(
            f"/v1/profiles/{profile_id}/models",
            headers={"Authorization": "Bearer short"},
        ).status_code
        == 503
    )


def test_nofx_response_schema_allows_free_text() -> None:
    response = TestClient(create_app(manager=Manager(), service_token=TOKEN)).post(
        f"/v1/profiles/{uuid4()}/generate",
        headers={"Authorization": f"Bearer {TOKEN}"},
        json={
            "system": "policy",
            "user": "snapshot",
            "output_schema": {
                "type": "object",
                "properties": {"response": {"type": "string"}},
                "required": ["response"],
                "additionalProperties": False,
            },
        },
    )
    assert response.status_code == 200
    assert response.json()["content"] == {"response": "free text"}


def test_upstream_error_detail_is_sanitized() -> None:
    class Broken:
        async def models(self) -> dict[str, Any]:
            raise CodexProtocolError("Codex RPC request failed")

    response = TestClient(
        create_app(manager=Manager(Broken()), service_token=TOKEN)
    ).get(
        f"/v1/profiles/{uuid4()}/models", headers={"Authorization": f"Bearer {TOKEN}"}
    )
    assert response.status_code == 502
    assert response.json() == {"detail": "Codex RPC request failed"}


def test_service_token_file_takes_priority_over_environment(
    tmp_path, monkeypatch
) -> None:
    token_file = tmp_path / "gateway-token"
    token_file.write_text(TOKEN)
    monkeypatch.setenv("CODEX_GATEWAY_SERVICE_TOKEN_FILE", str(token_file))
    monkeypatch.setenv(
        "CODEX_GATEWAY_SERVICE_TOKEN", "wrong-environment-token-with-32-chars"
    )
    assert _load_service_token(None) == TOKEN
