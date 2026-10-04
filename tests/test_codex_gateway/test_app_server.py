from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any
from unittest.mock import AsyncMock
from uuid import uuid4

import pytest

from apps.codex_gateway.app_server import (
    APP_SERVER_STREAM_LIMIT_BYTES,
    DISABLED_CODEX_FEATURES,
    ENABLED_CODEX_FEATURES,
    MAX_GENERATION_TIMEOUT_SECONDS,
    CodexProfile,
    CodexProfileManager,
    CodexProtocolError,
    JsonRpcSession,
)

pytestmark = pytest.mark.asyncio


class FakeRpc:
    def __init__(self) -> None:
        self.requests: list[tuple[str, dict[str, Any]]] = []
        self.notifications: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self.response_text = '{"response":"HOLD because risk is elevated."}'
        self.hang_on_turn = False
        self.notification_timeouts: list[float] = []

    async def start(self) -> None:
        pass

    async def close(self) -> None:
        pass

    async def request(
        self, method: str, params: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        params = params or {}
        self.requests.append((method, params))
        if method == "account/login/start":
            return {
                "loginId": "login-1",
                "verificationUrl": "https://auth.openai.com/codex/device",
                "userCode": "ABCD-1234",
            }
        if method == "account/read":
            return {
                "account": {
                    "type": "chatgpt",
                    "email": "private@example.test",
                    "planType": "plus",
                },
                "requiresOpenaiAuth": True,
            }
        if method == "model/list":
            return {
                "data": [
                    {
                        "id": "gpt-5.6-luna",
                        "supportedReasoningEfforts": [{"reasoningEffort": "low"}],
                    },
                    {
                        "id": "gpt-6.1-sol",
                        "defaultReasoningEffort": "low",
                        "supportedReasoningEfforts": [
                            {"reasoningEffort": "low"},
                            {"reasoningEffort": "high"},
                            {
                                "reasoningEffort": "ultra",
                                "description": "automatic delegation",
                            },
                        ],
                    },
                    {
                        "id": "gpt-6-sol",
                        "defaultReasoningEffort": "none",
                        "supportedReasoningEfforts": [],
                    },
                ],
                "nextCursor": None,
            }
        if method == "thread/start":
            return {"thread": {"id": "thread-1"}}
        if method == "turn/start":
            if not self.hang_on_turn:
                await self.notifications.put(
                    {
                        "method": "item/completed",
                        "params": {
                            "turnId": "turn-1",
                            "item": {
                                "type": "agentMessage",
                                "phase": "final_answer",
                                "text": self.response_text,
                            },
                        },
                    }
                )
                await self.notifications.put(
                    {
                        "method": "turn/completed",
                        "params": {"turn": {"id": "turn-1", "status": "completed"}},
                    }
                )
            return {"turn": {"id": "turn-1"}}
        if method in {
            "thread/delete",
            "turn/interrupt",
            "account/logout",
            "account/login/cancel",
        }:
            return {}
        if method == "account/rateLimits/read":
            return {"rateLimits": {}}
        raise AssertionError(method)

    async def next_notification(self, timeout: float) -> dict[str, Any]:
        self.notification_timeouts.append(timeout)
        return await asyncio.wait_for(self.notifications.get(), timeout)

    def drain_notifications(self) -> list[dict[str, Any]]:
        return []

    def requeue_notifications(self, notifications: list[dict[str, Any]]) -> None:
        for notification in notifications:
            self.notifications.put_nowait(notification)


@pytest.fixture
def schema() -> dict[str, Any]:
    return {
        "type": "object",
        "properties": {"response": {"type": "string"}},
        "required": ["response"],
        "additionalProperties": False,
    }


async def test_managed_device_login_and_sanitized_account(tmp_path: Path) -> None:
    rpc = FakeRpc()
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    login = await profile.start_device_login()
    account = await profile.account()
    assert login["loginId"] == "login-1"
    assert rpc.requests[0] == ("account/login/start", {"type": "chatgptDeviceCode"})
    assert account["account"] == {"type": "chatgpt", "planType": "plus"}
    assert "email" not in str(account)


async def test_catalog_drives_model_and_effort_selection(
    tmp_path: Path, schema: dict[str, Any]
) -> None:
    rpc = FakeRpc()
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    result = await profile.generate(
        system="Policy",
        user="Snapshot",
        model="gpt-6.1-sol",
        effort="high",
        output_schema=schema,
        timeout_seconds=1,
    )
    assert result.content == {"response": "HOLD because risk is elevated."}
    assert [method for method, _ in rpc.requests][:2] == ["model/list", "thread/start"]
    turn = next(params for method, params in rpc.requests if method == "turn/start")
    assert turn["approvalPolicy"] == "untrusted"
    assert turn["sandboxPolicy"] == {"type": "readOnly", "networkAccess": False}
    assert rpc.notification_timeouts == [MAX_GENERATION_TIMEOUT_SECONDS] * 2


async def test_catalog_sanitizes_ultra_and_defaults_empty_efforts_to_low(
    tmp_path: Path,
) -> None:
    rpc = FakeRpc()
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)

    result = await profile.models()

    by_id = {entry["id"]: entry for entry in result["data"]}
    assert by_id["gpt-6.1-sol"]["defaultReasoningEffort"] == "low"
    assert [
        effort["reasoningEffort"]
        for effort in by_id["gpt-6.1-sol"]["supportedReasoningEfforts"]
    ] == ["low", "high"]
    assert by_id["gpt-6-sol"]["defaultReasoningEffort"] == "low"
    assert by_id["gpt-6-sol"]["supportedReasoningEfforts"] == [
        {"reasoningEffort": "low"}
    ]


async def test_unavailable_model_and_effort_fail_before_thread(
    tmp_path: Path, schema: dict[str, Any]
) -> None:
    rpc = FakeRpc()
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    with pytest.raises(ValueError, match="model is not available"):
        await profile.generate(
            system="x", user="y", model="gpt-7", effort="low", output_schema=schema
        )
    with pytest.raises(ValueError, match="effort is not available"):
        await profile.generate(
            system="x",
            user="y",
            model="gpt-6.1-sol",
            effort="ultra",
            output_schema=schema,
        )
    assert "thread/start" not in [method for method, _ in rpc.requests]


async def test_timeout_interrupts_turn_and_deletes_thread(
    tmp_path: Path, schema: dict[str, Any]
) -> None:
    rpc = FakeRpc()
    rpc.hang_on_turn = True
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    with pytest.raises(CodexProtocolError, match="timed out"):
        await profile.generate(
            system="x", user="y", output_schema=schema, timeout_seconds=0.02
        )
    methods = [method for method, _ in rpc.requests]
    assert "turn/interrupt" in methods
    assert "thread/delete" in methods


async def test_unexpected_tool_request_fails_closed(
    tmp_path: Path, schema: dict[str, Any]
) -> None:
    rpc = FakeRpc()
    rpc.hang_on_turn = True
    await rpc.notifications.put(
        {"method": "gateway/unexpectedToolRequest", "params": {}}
    )
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    with pytest.raises(CodexProtocolError, match="disabled tool"):
        await profile.generate(
            system="x", user="y", output_schema=schema, timeout_seconds=1
        )
    methods = [method for method, _ in rpc.requests]
    assert methods[-2:] == ["turn/interrupt", "thread/delete"]


async def test_unexpected_output_interrupts_before_thread_delete(
    tmp_path: Path, schema: dict[str, Any]
) -> None:
    rpc = FakeRpc()
    rpc.hang_on_turn = True
    await rpc.notifications.put(
        {
            "method": "item/completed",
            "params": {"turnId": "turn-1", "item": {"type": "commandExecution"}},
        }
    )
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    with pytest.raises(CodexProtocolError, match="unexpected output"):
        await profile.generate(
            system="x", user="y", output_schema=schema, timeout_seconds=1
        )
    methods = [method for method, _ in rpc.requests]
    assert methods[-2:] == ["turn/interrupt", "thread/delete"]


async def test_account_drain_preserves_turn_notifications(tmp_path: Path) -> None:
    rpc = FakeRpc()
    pending = {"method": "turn/completed", "params": {"turn": {"id": "turn-1"}}}
    await rpc.notifications.put(pending)

    def drain() -> list[dict[str, Any]]:
        return [rpc.notifications.get_nowait()]

    rpc.drain_notifications = drain  # type: ignore[method-assign]
    profile = CodexProfile(profile_id=str(uuid4()), home=tmp_path, rpc=rpc)
    profile._drain_account_notifications()
    assert await rpc.next_notification(0.1) == pending


async def test_rpc_child_gets_allowlisted_environment_and_tool_disables(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    captured: dict[str, Any] = {}

    class Stdin:
        def write(self, _: bytes) -> None:
            pass

        async def drain(self) -> None:
            pass

    class Process:
        returncode = None
        stdin = Stdin()
        stdout = None

    async def spawn(*args: Any, **kwargs: Any) -> Process:
        captured["args"] = args
        captured["kwargs"] = kwargs
        return Process()

    async def request(
        self: JsonRpcSession,
        method: str,
        params: dict[str, Any] | None = None,
        *,
        allow_restart: bool = True,
    ) -> dict[str, Any]:
        return {}

    monkeypatch.setenv("CODEX_GATEWAY_SERVICE_TOKEN", "service-secret-never-forwarded")
    monkeypatch.setenv("BYBIT_API_KEY", "exchange-secret-never-forwarded")
    monkeypatch.setattr(asyncio, "create_subprocess_exec", spawn)
    monkeypatch.setattr(JsonRpcSession, "request", request)
    session = JsonRpcSession(codex_home=tmp_path / "profile")
    await session.start()
    child_env = captured["kwargs"]["env"]
    assert captured["kwargs"]["limit"] == APP_SERVER_STREAM_LIMIT_BYTES
    assert set(child_env) == {
        "PATH",
        "HOME",
        "CODEX_HOME",
        "TMPDIR",
        "LANG",
        "LC_ALL",
        "NO_COLOR",
    }
    assert "CODEX_GATEWAY_SERVICE_TOKEN" not in child_env
    assert "BYBIT_API_KEY" not in child_env
    args = captured["args"]
    assert "--strict-config" in args
    assert all(feature in args for feature in DISABLED_CODEX_FEATURES)
    assert all(feature in args for feature in ENABLED_CODEX_FEATURES)


async def test_disconnect_is_only_operation_that_removes_profile(
    tmp_path: Path,
) -> None:
    manager = CodexProfileManager(root=tmp_path)
    profile_id = str(uuid4())
    profile = manager.profile(profile_id)
    profile.home.mkdir(parents=True)
    profile.logout = AsyncMock()
    profile.close = AsyncMock()
    await manager.close()
    assert profile.home.exists()
    await manager.disconnect(profile_id)
    assert not profile.home.exists()


async def test_profile_path_is_canonical_uuid(tmp_path: Path) -> None:
    manager = CodexProfileManager(root=tmp_path)
    with pytest.raises(ValueError, match="profile_id"):
        manager.profile("../../escape")
