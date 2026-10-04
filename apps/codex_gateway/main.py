from __future__ import annotations

import os
from contextlib import asynccontextmanager
from hmac import compare_digest
from pathlib import Path
from typing import Any

from fastapi import Depends, FastAPI, Header, HTTPException, Query
from pydantic import BaseModel, Field

from .app_server import CodexProfile, CodexProfileManager, CodexProtocolError


def _load_service_token(explicit: str | None) -> str:
    if explicit is not None:
        return explicit
    token_file = os.getenv(
        "CODEX_GATEWAY_SERVICE_TOKEN_FILE", "/run/secrets/codex_gateway_token"
    )
    try:
        return Path(token_file).read_text(encoding="utf-8").strip()
    except (OSError, UnicodeError):
        return os.getenv("CODEX_GATEWAY_SERVICE_TOKEN", "")


class GenerateRequest(BaseModel):
    system: str = Field(min_length=1, max_length=100_000)
    user: str = Field(min_length=1, max_length=500_000)
    model: str = Field(default="gpt-5.6-luna", min_length=1, max_length=128)
    effort: str = Field(default="low", min_length=1, max_length=32)
    output_schema: dict[str, Any]
    timeout_seconds: float = Field(default=35, ge=1, le=120)


class GenerateResponse(BaseModel):
    content: dict[str, Any]
    model: str
    effort: str
    usage: dict[str, Any]
    latency_ms: int


class LoginCancelRequest(BaseModel):
    login_id: str = Field(min_length=1, max_length=200)


def create_app(
    *,
    manager: CodexProfileManager | Any | None = None,
    service_token: str | None = None,
) -> FastAPI:
    profile_manager = manager or CodexProfileManager()
    expected_token = _load_service_token(service_token)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        yield
        close = getattr(profile_manager, "close", None)
        if close is not None:
            await close()

    app = FastAPI(title="NOFX Codex Gateway", version="0.1.0", lifespan=lifespan)

    def require_service_token(authorization: str | None = Header(default=None)) -> None:
        if len(expected_token) < 32:
            raise HTTPException(
                status_code=503, detail="Gateway service token is not configured"
            )
        if authorization is None or not authorization.startswith("Bearer "):
            raise HTTPException(status_code=401, detail="Unauthorized")
        if not compare_digest(authorization.removeprefix("Bearer "), expected_token):
            raise HTTPException(status_code=401, detail="Unauthorized")

    def get_profile(profile_id: str) -> CodexProfile | Any:
        try:
            return profile_manager.profile(profile_id)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail="Invalid profile id") from exc

    async def call(awaitable: Any) -> Any:
        try:
            return await awaitable
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except CodexProtocolError as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.post(
        "/v1/profiles/{profile_id}/login/start",
        dependencies=[Depends(require_service_token)],
    )
    async def login_start(profile_id: str) -> dict[str, Any]:
        return await call(get_profile(profile_id).start_device_login())

    @app.get(
        "/v1/profiles/{profile_id}/login/status",
        dependencies=[Depends(require_service_token)],
    )
    async def login_status(
        profile_id: str, login_id: str = Query(min_length=1)
    ) -> dict[str, Any]:
        return await call(get_profile(profile_id).login_status(login_id))

    @app.post(
        "/v1/profiles/{profile_id}/login/cancel",
        dependencies=[Depends(require_service_token)],
    )
    async def login_cancel(
        profile_id: str, payload: LoginCancelRequest
    ) -> dict[str, bool]:
        await call(get_profile(profile_id).cancel_login(payload.login_id))
        return {"cancelled": True}

    @app.get(
        "/v1/profiles/{profile_id}/account",
        dependencies=[Depends(require_service_token)],
    )
    async def account(profile_id: str, refresh_token: bool = False) -> dict[str, Any]:
        return await call(get_profile(profile_id).account(refresh_token=refresh_token))

    @app.post(
        "/v1/profiles/{profile_id}/logout",
        dependencies=[Depends(require_service_token)],
    )
    async def logout(profile_id: str) -> dict[str, bool]:
        disconnect = getattr(profile_manager, "disconnect", None)
        if disconnect is not None:
            await call(disconnect(profile_id))
        else:
            await call(get_profile(profile_id).logout())
        return {"disconnected": True}

    @app.get(
        "/v1/profiles/{profile_id}/models",
        dependencies=[Depends(require_service_token)],
    )
    async def models(profile_id: str) -> dict[str, Any]:
        return await call(get_profile(profile_id).models())

    @app.get(
        "/v1/profiles/{profile_id}/rate-limits",
        dependencies=[Depends(require_service_token)],
    )
    async def rate_limits(profile_id: str) -> dict[str, Any]:
        return await call(get_profile(profile_id).rate_limits())

    @app.post(
        "/v1/profiles/{profile_id}/generate",
        response_model=GenerateResponse,
        dependencies=[Depends(require_service_token)],
    )
    async def generate(profile_id: str, payload: GenerateRequest) -> GenerateResponse:
        result = await call(get_profile(profile_id).generate(**payload.model_dump()))
        return GenerateResponse(
            content=result.content,
            model=result.model,
            effort=result.effort,
            usage=result.usage,
            latency_ms=result.elapsed_ms,
        )

    return app


app = create_app()
