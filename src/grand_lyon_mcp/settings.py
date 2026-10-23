"""Application settings from env + optional YAML."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

import yaml
from platformdirs import user_config_dir, user_data_dir
from pydantic import BaseModel, Field, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class JourneyScoringWeights(BaseModel):
    duration: float = Field(default=0.35, ge=0, le=1)
    reliability: float = Field(default=0.25, ge=0, le=1)
    disruptions: float = Field(default=0.15, ge=0, le=1)
    walking: float = Field(default=0.10, ge=0, le=1)
    availability: float = Field(default=0.10, ge=0, le=1)
    user_preference: float = Field(default=0.05, ge=0, le=1)

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
        env_ignore_empty=True,
        extra="ignore",
        populate_by_name=True,
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
    http_connect_timeout_seconds: float = Field(default=5.0, gt=0)
    http_read_timeout_seconds: float = Field(default=20.0, gt=0)
    max_parallel_requests: int = Field(default=6, ge=1)
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
    fuzzy_score_threshold: int = Field(default=72, ge=0, le=100)
    allow_stale_on_error: bool = True
    negative_ttl_seconds: int = 30
    journey_scoring: JourneyScoringWeights = Field(default_factory=JourneyScoringWeights)

    @field_validator("log_level")
    @classmethod
    def upper_level(cls, v: str) -> str:
        return v.upper()

    @field_validator("timezone")
    @classmethod
    def valid_timezone(cls, value: str) -> str:
        try:
            ZoneInfo(value)
        except ZoneInfoNotFoundError as exc:
            raise ValueError(f"Unknown timezone: {value}") from exc
        return value

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
        raise ValueError(f"Settings file must contain a mapping: {path}")
    return data


def _section(overlay: dict[str, Any], name: str) -> dict[str, Any]:
    section = overlay.get(name) or {}
    if not isinstance(section, dict):
        raise ValueError(f"Settings section must contain a mapping: {name}")
    return section


def _environment_has(field_name: str) -> bool:
    expected = f"GRAND_LYON_MCP_{field_name.upper()}"
    return expected in {key.upper() for key in os.environ}


def apply_settings_yaml(settings: Settings, config_dir: Path | None = None) -> Settings:
    """Merge the optional settings file into validated runtime settings.

    Environment variables have priority over all YAML values.
    """
    base = config_dir or settings.resolved_config_dir()
    overlay = load_yaml_overlay(base / "settings.yaml")
    if not overlay:
        return settings
    version = overlay.get("version", 1)
    if version != 1:
        raise ValueError(f"Unsupported settings version: {version}")

    runtime = _section(overlay, "runtime")
    results = _section(overlay, "results")
    places = _section(overlay, "places")
    cache = _section(overlay, "cache")
    network = _section(overlay, "network")
    updates: dict[str, Any] = {}
    yaml_fields = (
        (runtime, "offline", "offline"),
        (runtime, "timezone", "timezone"),
        (runtime, "locale", "locale"),
        (runtime, "max_parallel_requests", "max_parallel_requests"),
        (results, "maximum_items_per_tool", "maximum_items_per_tool"),
        (places, "default_radius_m", "default_radius_m"),
        (places, "maximum_radius_m", "maximum_radius_m"),
        (places, "fuzzy_score_threshold", "fuzzy_score_threshold"),
        (cache, "allow_stale_on_error", "allow_stale_on_error"),
        (cache, "negative_ttl_seconds", "negative_ttl_seconds"),
        (network, "connect_timeout_seconds", "http_connect_timeout_seconds"),
        (network, "read_timeout_seconds", "http_read_timeout_seconds"),
        (network, "verify_tls", "verify_tls"),
    )
    for section, yaml_name, field_name in yaml_fields:
        if yaml_name in section and not _environment_has(field_name):
            updates[field_name] = section[yaml_name]

    journey_scoring = overlay.get("journey_scoring")
    if journey_scoring is not None:
        if not isinstance(journey_scoring, dict):
            raise ValueError("Settings section must contain a mapping: journey_scoring")
        if not _environment_has("journey_scoring"):
            updates["journey_scoring"] = journey_scoring

    values = settings.model_dump()
    values.update(updates)
    return Settings.model_validate(values)


def get_settings() -> Settings:
    settings = Settings()
    return apply_settings_yaml(settings)
