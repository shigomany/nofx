from __future__ import annotations

import asyncio
import json
import os
import re
import shutil
import time
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol
from uuid import UUID

from jsonschema import ValidationError, validate

MODEL_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$")
EFFORT_RE = re.compile(r"^[a-z][a-z0-9_-]{0,31}$")
RPC_TIMEOUT_SECONDS = 15.0
CLEANUP_TIMEOUT_SECONDS = 2.0
MAX_GENERATION_TIMEOUT_SECONDS = 120.0
APP_SERVER_STREAM_LIMIT_BYTES = 1024 * 1024

# These names were verified with `@openai/codex@0.144.3 features list`.
# Unknown/removed features are intentionally omitted because app-server starts
# with --strict-config. MCP servers are absent because every profile gets a new,
# gateway-owned CODEX_HOME containing no inherited configuration.
DISABLED_CODEX_FEATURES = (
    "apps",
    "auth_elicitation",
    "browser_use",
    "browser_use_external",
    "browser_use_full_cdp_access",
    "computer_use",
    "image_generation",
    "memories",
    "multi_agent",
    "plugins",
    "remote_plugin",
    "shell_snapshot",
    "shell_tool",
    "skill_mcp_dependency_install",
    "standalone_web_search",
    "tool_call_mcp_elicitation",
    "tool_suggest",
    "web_search_request",
    "workspace_dependencies",
)


class CodexProtocolError(RuntimeError):
    """A deliberately sanitized App Server failure safe for an HTTP response."""


class RpcSession(Protocol):
    async def start(self) -> None: ...
    async def close(self) -> None: ...
    async def request(
        self, method: str, params: dict[str, Any] | None = None
    ) -> dict[str, Any]: ...
    async def next_notification(self, timeout: float) -> dict[str, Any]: ...
    def drain_notifications(self) -> list[dict[str, Any]]: ...
    def requeue_notifications(self, notifications: list[dict[str, Any]]) -> None: ...


class JsonRpcSession:
    def __init__(self, *, codex_home: Path, codex_binary: str = "codex") -> None:
        self._codex_home = codex_home
        self._codex_binary = codex_binary
        self._process: asyncio.subprocess.Process | None = None
        self._reader_task: asyncio.Task[None] | None = None
        self._pending: dict[int, asyncio.Future[dict[str, Any]]] = {}
        self._notifications: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self._next_id = 1
        self._write_lock = asyncio.Lock()
        self._start_lock = asyncio.Lock()

    async def start(self) -> None:
        if self._process is not None and self._process.returncode is None:
            return
        async with self._start_lock:
            if self._process is not None and self._process.returncode is None:
                return
            await self._discard_dead_process()
            self._codex_home.mkdir(parents=True, exist_ok=True, mode=0o700)
            self._codex_home.chmod(0o700)
            temp_dir = self._codex_home / "tmp"
            temp_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
            temp_dir.chmod(0o700)
            env = {
                "PATH": os.environ.get("PATH", "/usr/local/bin:/usr/bin:/bin"),
                "HOME": str(self._codex_home),
                "CODEX_HOME": str(self._codex_home),
                "TMPDIR": str(temp_dir),
                "LANG": "C.UTF-8",
                "LC_ALL": "C.UTF-8",
                "NO_COLOR": "1",
            }
            args = [
                self._codex_binary,
                "app-server",
                "--listen",
                "stdio://",
                "--strict-config",
            ]
            for feature in DISABLED_CODEX_FEATURES:
                args.extend(("--disable", feature))
            try:
                self._process = await asyncio.create_subprocess_exec(
                    *args,
                    stdin=asyncio.subprocess.PIPE,
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.DEVNULL,
                    env=env,
                    limit=APP_SERVER_STREAM_LIMIT_BYTES,
                )
            except OSError as exc:
                raise CodexProtocolError("Codex App Server is unavailable") from exc
            self._reader_task = asyncio.create_task(self._read_loop())
            try:
                await self.request(
                    "initialize",
                    {
                        "clientInfo": {
                            "name": "nofx_gateway",
                            "title": "NOFX Codex Gateway",
                            "version": "0.1.0",
                        },
                        "capabilities": {
                            "optOutNotificationMethods": [
                                "item/agentMessage/delta",
                                "item/reasoning/summaryTextDelta",
                                "item/reasoning/textDelta",
                            ]
                        },
                    },
                    allow_restart=False,
                )
                await self._write({"method": "initialized", "params": {}})
            except Exception:
                await self.close()
                raise

    async def _discard_dead_process(self) -> None:
        if self._reader_task is not None:
            self._reader_task.cancel()
            await asyncio.gather(self._reader_task, return_exceptions=True)
        self._process = None
        self._reader_task = None

    async def close(self) -> None:
        process = self._process
        if process is not None and process.returncode is None:
            process.terminate()
            try:
                await asyncio.wait_for(process.wait(), timeout=2)
            except TimeoutError:
                process.kill()
                await process.wait()
        await self._discard_dead_process()

    async def request(
        self,
        method: str,
        params: dict[str, Any] | None = None,
        *,
        allow_restart: bool = True,
    ) -> dict[str, Any]:
        process = self._process
        if process is None or process.returncode is not None:
            if not allow_restart:
                raise CodexProtocolError("Codex App Server failed to start")
            await self.start()
            process = self._process
        if process is None or process.returncode is not None:
            raise CodexProtocolError("Codex App Server is unavailable")
        request_id = self._next_id
        self._next_id += 1
        future: asyncio.Future[dict[str, Any]] = (
            asyncio.get_running_loop().create_future()
        )
        self._pending[request_id] = future
        try:
            await self._write(
                {"method": method, "id": request_id, "params": params or {}}
            )
            return await asyncio.wait_for(future, timeout=RPC_TIMEOUT_SECONDS)
        except TimeoutError as exc:
            raise CodexProtocolError("Codex RPC timed out") from exc
        finally:
            self._pending.pop(request_id, None)

    async def next_notification(self, timeout: float) -> dict[str, Any]:
        return await asyncio.wait_for(self._notifications.get(), timeout=timeout)

    def drain_notifications(self) -> list[dict[str, Any]]:
        result: list[dict[str, Any]] = []
        while not self._notifications.empty():
            result.append(self._notifications.get_nowait())
        return result

    def requeue_notifications(self, notifications: list[dict[str, Any]]) -> None:
        for notification in notifications:
            self._notifications.put_nowait(notification)

    async def _write(self, payload: Mapping[str, Any]) -> None:
        process = self._process
        if process is None or process.stdin is None or process.returncode is not None:
            raise CodexProtocolError("Codex App Server transport is closed")
        encoded = json.dumps(payload, separators=(",", ":")).encode() + b"\n"
        async with self._write_lock:
            process.stdin.write(encoded)
            await process.stdin.drain()

    async def _read_loop(self) -> None:
        process = self._process
        if process is None or process.stdout is None:
            return
        try:
            while line := await process.stdout.readline():
                try:
                    message = json.loads(line)
                except (json.JSONDecodeError, UnicodeDecodeError):
                    continue
                request_id = message.get("id")
                if request_id is not None and (
                    "result" in message or "error" in message
                ):
                    future = self._pending.get(int(request_id))
                    if future is None or future.done():
                        continue
                    if "error" in message:
                        future.set_exception(
                            CodexProtocolError("Codex RPC request failed")
                        )
                    else:
                        result = message.get("result")
                        future.set_result(result if isinstance(result, dict) else {})
                elif request_id is not None and message.get("method"):
                    await self._decline_server_request(message)
                    await self._notifications.put(
                        {"method": "gateway/unexpectedToolRequest", "params": {}}
                    )
                elif message.get("method"):
                    await self._notifications.put(message)
        finally:
            error = CodexProtocolError("Codex App Server transport closed")
            for future in self._pending.values():
                if not future.done():
                    future.set_exception(error)

    async def _decline_server_request(self, message: dict[str, Any]) -> None:
        method = str(message.get("method") or "")
        if method.endswith("requestApproval"):
            result: dict[str, Any] = {"decision": "decline"}
        else:
            result = {"action": "decline", "content": None}
        await self._write({"id": message.get("id"), "result": result})


@dataclass(frozen=True)
class CodexGenerationResult:
    content: dict[str, Any]
    model: str
    effort: str
    usage: dict[str, Any]
    elapsed_ms: int


class CodexProfile:
    def __init__(
        self, *, profile_id: str, home: Path, rpc: RpcSession | None = None
    ) -> None:
        self.profile_id = profile_id
        self._home = home
        self._codex_home = home / "codex-home"
        self._workspace = home / "workspace"
        self._rpc = rpc or JsonRpcSession(codex_home=self._codex_home)
        self._started = False
        self._start_lock = asyncio.Lock()
        self._generation_lock = asyncio.Lock()
        self._login_states: dict[str, dict[str, Any]] = {}

    @property
    def home(self) -> Path:
        return self._home

    async def _ensure_started(self) -> None:
        async with self._start_lock:
            if self._started:
                return
            self._home.mkdir(parents=True, exist_ok=True, mode=0o700)
            self._workspace.mkdir(parents=True, exist_ok=True, mode=0o700)
            self._home.chmod(0o700)
            self._workspace.chmod(0o700)
            await self._rpc.start()
            self._started = True

    async def close(self) -> None:
        await self._rpc.close()
        self._started = False

    async def start_device_login(self) -> dict[str, Any]:
        await self._ensure_started()
        result = await self._rpc.request(
            "account/login/start", {"type": "chatgptDeviceCode"}
        )
        login_id = str(result.get("loginId") or "")
        if not login_id:
            raise CodexProtocolError("Codex device login could not be started")
        self._login_states[login_id] = {
            "loginId": login_id,
            "status": "pending",
            "error": None,
        }
        return {
            "type": "chatgptDeviceCode",
            "loginId": login_id,
            "verificationUrl": str(result.get("verificationUrl") or ""),
            "userCode": str(result.get("userCode") or ""),
        }

    async def login_status(self, login_id: str) -> dict[str, Any]:
        await self._ensure_started()
        self._drain_account_notifications()
        return self._login_states.get(
            login_id, {"loginId": login_id, "status": "unknown", "error": None}
        )

    async def cancel_login(self, login_id: str) -> None:
        await self._ensure_started()
        await self._rpc.request("account/login/cancel", {"loginId": login_id})
        self._login_states[login_id] = {
            "loginId": login_id,
            "status": "cancelled",
            "error": None,
        }

    async def account(self, *, refresh_token: bool = False) -> dict[str, Any]:
        await self._ensure_started()
        result = await self._rpc.request(
            "account/read", {"refreshToken": refresh_token}
        )
        account = result.get("account")
        safe = (
            {"type": account.get("type"), "planType": account.get("planType")}
            if isinstance(account, dict)
            else None
        )
        return {
            "account": safe,
            "requiresOpenaiAuth": bool(result.get("requiresOpenaiAuth", True)),
        }

    async def logout(self) -> None:
        await self._ensure_started()
        await self._rpc.request("account/logout")
        self._login_states.clear()

    async def models(self) -> dict[str, Any]:
        await self._ensure_started()
        result = await self._rpc.request(
            "model/list", {"limit": 100, "includeHidden": False}
        )
        data = [
            _sanitize_model(entry)
            for entry in (result.get("data") or [])
            if isinstance(entry, dict) and _model_id(entry)
        ]
        return {"data": data, "nextCursor": result.get("nextCursor")}

    async def rate_limits(self) -> dict[str, Any]:
        await self._ensure_started()
        return await self._rpc.request("account/rateLimits/read")

    async def generate(
        self,
        *,
        system: str,
        user: str,
        model: str = "gpt-5.6-luna",
        effort: str = "low",
        output_schema: dict[str, Any],
        timeout_seconds: float = 35,
    ) -> CodexGenerationResult:
        if not MODEL_ID_RE.fullmatch(model):
            raise ValueError("Invalid Codex model id")
        if not EFFORT_RE.fullmatch(effort):
            raise ValueError("Invalid Codex reasoning effort")
        if not output_schema or output_schema.get("type") != "object":
            raise ValueError("output_schema must describe a JSON object")
        try:
            return await asyncio.wait_for(
                self._generate_serialized(
                    system=system,
                    user=user,
                    model=model,
                    effort=effort,
                    output_schema=output_schema,
                ),
                timeout=timeout_seconds,
            )
        except TimeoutError as exc:
            raise CodexProtocolError("Codex generation timed out") from exc

    async def _generate_serialized(
        self,
        *,
        system: str,
        user: str,
        model: str,
        effort: str,
        output_schema: dict[str, Any],
    ) -> CodexGenerationResult:
        await self._ensure_started()
        async with self._generation_lock:
            return await self._generate(
                system=system,
                user=user,
                model=model,
                effort=effort,
                output_schema=output_schema,
            )

    async def _generate(
        self,
        *,
        system: str,
        user: str,
        model: str,
        effort: str,
        output_schema: dict[str, Any],
    ) -> CodexGenerationResult:
        started = time.monotonic()
        thread_id = ""
        turn_id = ""
        usage: dict[str, Any] = {}
        final_text = ""
        turn_finished = False
        try:
            catalog = await self.models()
            selected = next(
                (entry for entry in catalog["data"] if _model_id(entry) == model), None
            )
            if selected is None:
                raise ValueError(
                    "Requested Codex model is not available for this profile"
                )
            supported = _supported_efforts(selected)
            if supported and effort not in supported:
                raise ValueError(
                    "Requested reasoning effort is not available for this model"
                )
            self._drain_account_notifications()
            thread_result = await self._rpc.request(
                "thread/start",
                {
                    "model": model,
                    "cwd": str(self._workspace),
                    "approvalPolicy": "untrusted",
                    "sandbox": "read-only",
                    "serviceName": "nofx_gateway",
                },
            )
            thread_id = str((thread_result.get("thread") or {}).get("id") or "")
            if not thread_id:
                raise CodexProtocolError("Codex did not create a thread")
            turn_result = await self._rpc.request(
                "turn/start",
                {
                    "threadId": thread_id,
                    "input": [
                        {
                            "type": "text",
                            "text": self._structured_prompt(system=system, user=user),
                        }
                    ],
                    "cwd": str(self._workspace),
                    "approvalPolicy": "untrusted",
                    "sandboxPolicy": {"type": "readOnly", "networkAccess": False},
                    "model": model,
                    "effort": effort,
                    "summary": "concise",
                    "outputSchema": output_schema,
                },
            )
            turn_id = str((turn_result.get("turn") or {}).get("id") or "")
            if not turn_id:
                raise CodexProtocolError("Codex did not create a turn")
            while True:
                # The outer generate() wait_for owns the total request budget.
                # Deltas are disabled, so a valid reasoning turn may otherwise
                # be completely quiet for longer than the normal RPC timeout.
                notification = await self._rpc.next_notification(
                    MAX_GENERATION_TIMEOUT_SECONDS
                )
                self._handle_account_notification(notification)
                method = str(notification.get("method") or "")
                if method == "gateway/unexpectedToolRequest":
                    raise CodexProtocolError("Codex attempted a disabled tool request")
                params = notification.get("params") or {}
                notification_turn_id = str(
                    params.get("turnId") or (params.get("turn") or {}).get("id") or ""
                )
                if notification_turn_id and notification_turn_id != turn_id:
                    continue
                if method == "item/completed":
                    item = params.get("item") or {}
                    item_type = item.get("type")
                    if item_type in {"userMessage", "reasoning"}:
                        continue
                    if item_type != "agentMessage":
                        raise CodexProtocolError("Codex returned unexpected output")
                    if item.get("phase") in {None, "final_answer"}:
                        final_text = str(item.get("text") or "")
                elif method == "thread/tokenUsage/updated":
                    usage = dict(params)
                    usage.pop("threadId", None)
                    usage.pop("turnId", None)
                elif method == "turn/completed":
                    turn_finished = True
                    if (params.get("turn") or {}).get("status") != "completed":
                        raise CodexProtocolError("Codex generation failed")
                    break
            if not final_text:
                raise CodexProtocolError("Codex returned no structured output")
            try:
                content = json.loads(final_text)
            except json.JSONDecodeError as exc:
                raise CodexProtocolError("Codex returned invalid JSON") from exc
            if not isinstance(content, dict):
                raise CodexProtocolError("Codex output is not an object")
            try:
                validate(instance=content, schema=output_schema)
            except ValidationError as exc:
                raise CodexProtocolError(
                    "Codex output failed schema validation"
                ) from exc
            return CodexGenerationResult(
                content=content,
                model=model,
                effort=effort,
                usage=usage,
                elapsed_ms=int((time.monotonic() - started) * 1000),
            )
        except (Exception, asyncio.CancelledError):
            if thread_id and turn_id and not turn_finished:
                await self._bounded_request(
                    "turn/interrupt", {"threadId": thread_id, "turnId": turn_id}
                )
            raise
        finally:
            if thread_id:
                await self._bounded_request("thread/delete", {"threadId": thread_id})

    async def _bounded_request(self, method: str, params: dict[str, Any]) -> None:
        try:
            await asyncio.wait_for(
                asyncio.shield(self._rpc.request(method, params)),
                timeout=CLEANUP_TIMEOUT_SECONDS,
            )
        except (TimeoutError, CodexProtocolError):
            pass

    @staticmethod
    def _structured_prompt(*, system: str, user: str) -> str:
        return (
            "You are an isolated trading inference component. Never call tools, execute commands, "
            "read files, browse, search, use MCP, or request input. Return only the JSON object "
            "required by the supplied schema. The response field may contain free text.\n\n"
            f"<policy>\n{system}\n</policy>\n\n<input>\n{user}\n</input>"
        )

    def _drain_account_notifications(self) -> None:
        retained: list[dict[str, Any]] = []
        for notification in self._rpc.drain_notifications():
            if notification.get("method") == "account/login/completed":
                self._handle_account_notification(notification)
            else:
                retained.append(notification)
        self._rpc.requeue_notifications(retained)

    def _handle_account_notification(self, notification: dict[str, Any]) -> None:
        if notification.get("method") != "account/login/completed":
            return
        params = notification.get("params") or {}
        login_id = str(params.get("loginId") or "")
        if login_id:
            self._login_states[login_id] = {
                "loginId": login_id,
                "status": "connected" if params.get("success") else "failed",
                "error": None if params.get("success") else "Login failed",
            }


def _model_id(entry: dict[str, Any]) -> str:
    value = str(entry.get("id") or entry.get("model") or "")
    return value if MODEL_ID_RE.fullmatch(value) else ""


def _supported_efforts(entry: dict[str, Any]) -> set[str]:
    values = entry.get("supportedReasoningEfforts") or []
    return {
        str(item.get("reasoningEffort") or item.get("effort") or "")
        for item in values
        if isinstance(item, dict)
    }


def _sanitize_model(entry: dict[str, Any]) -> dict[str, Any]:
    return {
        key: entry[key]
        for key in (
            "id",
            "model",
            "displayName",
            "isDefault",
            "supportedReasoningEfforts",
        )
        if key in entry
    }


class CodexProfileManager:
    def __init__(self, *, root: Path | None = None) -> None:
        self._root = root or Path(
            os.getenv("CODEX_PROFILES_ROOT", "/var/lib/codex-profiles")
        )
        self._profiles: dict[str, CodexProfile] = {}

    def profile(self, profile_id: str) -> CodexProfile:
        try:
            canonical = str(UUID(profile_id))
        except (ValueError, AttributeError) as exc:
            raise ValueError("profile_id must be a UUID") from exc
        if canonical != profile_id.lower():
            raise ValueError("profile_id must be a canonical UUID")
        if canonical not in self._profiles:
            self._profiles[canonical] = CodexProfile(
                profile_id=canonical, home=self._root / canonical
            )
        return self._profiles[canonical]

    async def close(self) -> None:
        await asyncio.gather(
            *(profile.close() for profile in self._profiles.values()),
            return_exceptions=True,
        )

    async def disconnect(self, profile_id: str) -> None:
        profile = self.profile(profile_id)
        await profile.logout()
        await profile.close()
        self._profiles.pop(profile_id, None)
        shutil.rmtree(profile.home, ignore_errors=True)
