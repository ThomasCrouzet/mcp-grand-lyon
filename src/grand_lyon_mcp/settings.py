"""Application settings from env + optional YAML."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from platformdirs import user_config_dir, user_data_dir
from pydantic import BaseModel, Field, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class JourneyScoringWeights(BaseModel):
    duration: float = 0.35
    reliability: float = 0.25
    disruptions: float = 0.15
    walking: float = 0.10
    availability: float = 0.10
    user_preference: float = 0.05

    @model_validator(mode="after")
    def sum_to_one(self) -> JourneyScoringWeights:
        total = (
            self.duration
            + self.reliability
            + self.disruptions
            + self.walking
            + self.availability
            + self.user_preference
        )
        if abs(total - 1.0) > 1e-6:
            raise ValueError(f"journey_scoring weights must sum to 1.0, got {total}")
        return self


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="GRAND_LYON_MCP_",
        env_file=None,  # do not auto-load repo .env in production
        extra="ignore",
    )

    # Credentials via separate env names (not GRAND_LYON_MCP_*)
    datagrandlyon_username: str = Field(default="", validation_alias="DATAGRANDLYON_USERNAME")
    datagrandlyon_password: str = Field(default="", validation_alias="DATAGRANDLYON_PASSWORD")

    config_dir: Path | None = None
    data_dir: Path | None = None
    db_path: Path | None = None
    fixtures_dir: Path | None = None
    log_level: str = "INFO"
    offline: bool = False
    http_connect_timeout_seconds: float = 5.0
    http_read_timeout_seconds: float = 20.0
    max_parallel_requests: int = 6
    verify_tls: bool = True

    transitous_enabled: bool = Field(default=False, validation_alias="TRANSITOUS_ENABLED")
    transitous_base_url: str = Field(
        default="https://api.transitous.org/api/",
        validation_alias="TRANSITOUS_BASE_URL",
    )

    timezone: str = "Europe/Paris"
    locale: str = "fr-FR"
    maximum_items_per_tool: int = 20
    maximum_warning_count: int = 20
    default_radius_m: int = 1000
    maximum_radius_m: int = 5000
    fuzzy_score_threshold: int = 72
    allow_stale_on_error: bool = True
    negative_ttl_seconds: int = 30
    journey_scoring: JourneyScoringWeights = Field(default_factory=JourneyScoringWeights)

    @field_validator("log_level")
    @classmethod
    def upper_level(cls, v: str) -> str:
        return v.upper()

    def resolved_config_dir(self) -> Path:
        if self.config_dir is not None:
            return Path(self.config_dir)
        return Path(user_config_dir("grand-lyon-mcp", "grand-lyon-mcp"))

    def resolved_data_dir(self) -> Path:
        if self.data_dir is not None:
            return Path(self.data_dir)
        return Path(user_data_dir("grand-lyon-mcp", "grand-lyon-mcp"))

    def resolved_db_path(self) -> Path:
        if self.db_path is not None:
            return Path(self.db_path)
        return self.resolved_data_dir() / "grand_lyon_mcp.db"

    def has_credentials(self) -> bool:
        return bool(self.datagrandlyon_username and self.datagrandlyon_password)

    def secret_values(self) -> list[str]:
        secrets: list[str] = []
        if self.datagrandlyon_username:
            secrets.append(self.datagrandlyon_username)
        if self.datagrandlyon_password:
            secrets.append(self.datagrandlyon_password)
        return secrets


def load_yaml_overlay(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {}
    with path.open(encoding="utf-8") as fh:
        data = yaml.safe_load(fh) or {}
    if not isinstance(data, dict):
        return {}
    return data


def apply_settings_yaml(settings: Settings, config_dir: Path | None = None) -> Settings:
    """Merge optional settings.yaml from config dir into Settings (shallow).

    Environment variables always win over YAML for runtime.offline and paths.
    """
    import os

    base = config_dir or settings.resolved_config_dir()
    overlay = load_yaml_overlay(base / "settings.yaml")
    if not overlay:
        return settings
    runtime = overlay.get("runtime") or {}
    results = overlay.get("results") or {}
    places = overlay.get("places") or {}
    cache = overlay.get("cache") or {}
    network = overlay.get("network") or {}
    updates: dict[str, Any] = {}
    # Env GRAND_LYON_MCP_OFFLINE takes precedence over YAML
    if "offline" in runtime and "GRAND_LYON_MCP_OFFLINE" not in os.environ:
        updates["offline"] = bool(runtime["offline"])
    if "timezone" in runtime:
        updates["timezone"] = str(runtime["timezone"])
    if "locale" in runtime:
        updates["locale"] = str(runtime["locale"])
    if "max_parallel_requests" in runtime:
        updates["max_parallel_requests"] = int(runtime["max_parallel_requests"])
    if "maximum_items_per_tool" in results:
        updates["maximum_items_per_tool"] = int(results["maximum_items_per_tool"])
    if "default_radius_m" in places:
        updates["default_radius_m"] = int(places["default_radius_m"])
    if "maximum_radius_m" in places:
        updates["maximum_radius_m"] = int(places["maximum_radius_m"])
    if "fuzzy_score_threshold" in places:
        updates["fuzzy_score_threshold"] = int(places["fuzzy_score_threshold"])
    if "allow_stale_on_error" in cache:
        updates["allow_stale_on_error"] = bool(cache["allow_stale_on_error"])
    if "negative_ttl_seconds" in cache:
        updates["negative_ttl_seconds"] = int(cache["negative_ttl_seconds"])
    if "connect_timeout_seconds" in network:
        updates["http_connect_timeout_seconds"] = float(network["connect_timeout_seconds"])
    if "read_timeout_seconds" in network:
        updates["http_read_timeout_seconds"] = float(network["read_timeout_seconds"])
    if "verify_tls" in network:
        updates["verify_tls"] = bool(network["verify_tls"])
    if "journey_scoring" in overlay and isinstance(overlay["journey_scoring"], dict):
        updates["journey_scoring"] = JourneyScoringWeights(**overlay["journey_scoring"])
    if updates:
        return settings.model_copy(update=updates)
    return settings


def get_settings() -> Settings:
    settings = Settings()
    return apply_settings_yaml(settings)
