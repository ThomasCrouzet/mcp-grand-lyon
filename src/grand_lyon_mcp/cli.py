"""Typer CLI entrypoint: grand-lyon-mcp."""

from __future__ import annotations

import asyncio
import json
from pathlib import Path

import typer

from grand_lyon_mcp.infrastructure.logging import setup_logging
from grand_lyon_mcp.infrastructure.redaction import redact_string
from grand_lyon_mcp.settings import get_settings
from grand_lyon_mcp.version import __version__

app = typer.Typer(
    name="grand-lyon-mcp",
    help="Serveur MCP local — services Métropole de Lyon / Grand Lyon.",
    no_args_is_help=True,
    add_completion=False,
)
catalog_app = typer.Typer(help="Catalogue DataGrandLyon")
db_app = typer.Typer(help="Base SQLite locale")
sync_app = typer.Typer(help="Synchronisation des données")
snapshot_app = typer.Typer(help="Snapshots")
app.add_typer(catalog_app, name="catalog")
app.add_typer(db_app, name="db")
app.add_typer(sync_app, name="sync")
app.add_typer(snapshot_app, name="snapshot")


def _run(coro: object) -> object:
    return asyncio.run(coro)  # type: ignore[arg-type]


@app.command()
def version() -> None:
    """Affiche la version du package."""
    typer.echo(__version__)


@app.command()
def serve(
    transport: str = typer.Option("stdio", "--transport", help="Transport MCP (stdio)"),
    quiet: bool = typer.Option(
        False,
        "--quiet",
        "-q",
        help="Ne pas afficher le bandeau de démarrage sur stderr",
    ),
) -> None:
    """Démarre le serveur MCP (stdio).

    N'affiche quasi rien sur le terminal : stdout = protocole JSON-RPC MCP.
    Les infos de démarrage vont sur stderr. Pour tester les outils sans client :
    `grand-lyon-mcp smoke` ou `make smoke`.
    """
    if transport != "stdio":
        typer.echo(f"Transport non supporté dans v1: {transport}", err=True)
        raise typer.Exit(2)
    setup_logging(get_settings().log_level)
    from grand_lyon_mcp.adapters.mcp import server as mcp_server

    mcp_server._quiet_banner = quiet

    try:
        asyncio.run(mcp_server.run_stdio())
    except KeyboardInterrupt:
        typer.echo("\ngrand-lyon-mcp: arrêté.", err=True)
        raise typer.Exit(0) from None
    except BrokenPipeError:
        raise typer.Exit(0) from None


@app.command()
def doctor() -> None:
    """Vérifie l'installation (sans révéler de secrets).

    Distingue clairement ``auth: OK`` d'un ``catalog_list: FORBIDDEN`` (403 métier)
    et le statut par source ``OK|UNRESOLVED|ERROR|DISABLED``.
    """
    settings = get_settings()
    setup_logging(settings.log_level)
    lines: list[str] = []

    def row(check: str, status: str, detail: str = "") -> None:
        lines.append(f"{status:12} {check}" + (f" — {detail}" if detail else ""))

    # credentials presence only
    if settings.has_credentials():
        row("DATAGRANDLYON credentials", "OK", "present (values hidden)")
        row("auth", "OK", "credentials configured")
    else:
        row("DATAGRANDLYON credentials", "WARNING", "missing (offline/fixture mode)")
        row("auth", "SKIPPED", "no credentials")

    config_dir = settings.resolved_config_dir()
    try:
        config_dir.mkdir(parents=True, exist_ok=True)
        row("config directory", "OK", str(config_dir))
    except OSError as exc:
        row("config directory", "ERROR", redact_string(str(exc)))

    data_dir = settings.resolved_data_dir()
    try:
        data_dir.mkdir(parents=True, exist_ok=True)
        row("data directory", "OK", str(data_dir))
    except OSError as exc:
        row("data directory", "ERROR", redact_string(str(exc)))

    async def _db_checks() -> None:
        from grand_lyon_mcp.storage.database import connect_and_migrate
        from grand_lyon_mcp.storage.migrations import db_info

        try:
            conn = await connect_and_migrate(settings.resolved_db_path())
            info = await db_info(conn)
            row("SQLite database", "OK", str(settings.resolved_db_path()))
            row("FTS5", "OK" if info["fts5"] else "ERROR")
            row("RTree", "OK" if info["rtree"] else "ERROR")
            row("migrations", "OK", f"{info['migration_count']} applied")
            await conn.close()
        except Exception as exc:
            row("SQLite database", "ERROR", redact_string(str(exc)))

    _run(_db_checks())

    row("offline mode", "OK" if not settings.offline else "WARNING", f"offline={settings.offline}")

    try:
        import mcp

        row("MCP SDK", "OK", getattr(mcp, "__version__", "installed"))
    except ImportError:
        row("MCP SDK", "ERROR", "not installed")

    if settings.transitous_enabled:
        row("Transitous", "OK", "enabled")
    else:
        row("Transitous", "DISABLED", "feature flag off")

    # auth probe + catalog list (403 is métier, not auth failure) + source registry
    async def _sources_and_catalog() -> None:
        from grand_lyon_mcp.bootstrap import build_app
        from grand_lyon_mcp.providers.datagrandlyon.catalog import CATALOG_URLS

        if settings.offline or not settings.has_credentials():
            row("catalog_list", "SKIPPED", "offline or no credentials")
        else:
            app_probe = await build_app(settings)
            try:
                # lightweight auth check: any authenticated call that is not catalog list
                # We probe rdata catalog; 401 → auth fail, 403 → FORBIDDEN (métier)
                import httpx

                url = CATALOG_URLS["rdata"]
                try:
                    from grand_lyon_mcp.providers.datagrandlyon.catalog_scan import (
                        classify_catalog_http_status,
                    )

                    resp = await app_probe.dgl.http.get(url, auth=app_probe.dgl._auth)
                    classified = classify_catalog_http_status(resp.status_code)
                    row("auth", classified["auth"], classified["detail"])
                    row("catalog_list", classified["catalog_list"], classified["detail"])
                except httpx.HTTPError as exc:
                    row("catalog_list", "ERROR", redact_string(str(exc)))
            finally:
                await app_probe.aclose()

        from grand_lyon_mcp.bootstrap import build_app as _ba

        app_c = await _ba(settings)
        try:
            sources = await app_c.source_registry.list_all()
            if not sources:
                row("source registry", "UNRESOLVED", "empty — run catalog scan")
            else:
                for s in sources:
                    st = s.get("status") or "UNRESOLVED"
                    if not s.get("enabled"):
                        st = "DISABLED"
                    row(f"source:{s['source_id']}", str(st))
        finally:
            await app_c.aclose()

    try:
        _run(_sources_and_catalog())
    except Exception as exc:
        row("source registry", "ERROR", redact_string(str(exc)))

    # never print secret values
    text = "\n".join(lines)
    for secret in settings.secret_values():
        text = text.replace(secret, "[REDACTED]")
    typer.echo(text)


@db_app.command("migrate")
def db_migrate() -> None:
    """Applique les migrations SQLite."""
    settings = get_settings()

    async def _go() -> list[str]:
        from grand_lyon_mcp.storage.database import connect
        from grand_lyon_mcp.storage.migrations import migrate

        conn = await connect(settings.resolved_db_path())
        try:
            return await migrate(conn)
        finally:
            await conn.close()

    applied = _run(_go())
    typer.echo(json.dumps({"applied": applied, "db": str(settings.resolved_db_path())}))


@db_app.command("info")
def db_info_cmd() -> None:
    """Informations sur la base locale."""
    settings = get_settings()

    async def _go() -> dict[str, object]:
        from grand_lyon_mcp.storage.database import connect_and_migrate
        from grand_lyon_mcp.storage.migrations import db_info

        conn = await connect_and_migrate(settings.resolved_db_path())
        try:
            info = await db_info(conn)
            info["path"] = str(settings.resolved_db_path())
            return info
        finally:
            await conn.close()

    typer.echo(json.dumps(_run(_go()), indent=2, default=str))


@catalog_app.command("scan")
def catalog_scan(
    mode: str = typer.Option(
        "auto",
        "--mode",
        help="Scan mode: auto (full→known+ogc on 403) | full | known | ogc",
    ),
) -> None:
    """Découverte de sources (hybride 403-safe).

    Modes:
    - ``auto`` : catalogue complet, bascule known+OGC si 403
    - ``full`` : uniquement listing catalogue
    - ``known`` : tables de ``config/sources.known.yaml`` + probe borné
    - ``ogc`` : index OGC public → candidats → validation DataPusher maxfeatures=1
    """
    settings = get_settings()
    if settings.offline:
        typer.echo(json.dumps({"status": "DISABLED", "reason": "offline mode"}))
        raise typer.Exit(0)

    mode_l = mode.lower().strip()
    if mode_l not in {"auto", "full", "known", "ogc"}:
        typer.echo(json.dumps({"status": "ERROR", "reason": f"unknown mode: {mode}"}))
        raise typer.Exit(2)

    async def _go() -> dict[str, object]:
        from grand_lyon_mcp.bootstrap import build_app
        from grand_lyon_mcp.providers.datagrandlyon.catalog import fetch_catalog, search_catalog
        from grand_lyon_mcp.providers.datagrandlyon.datapusher import query_table
        from grand_lyon_mcp.providers.live_providers import (
            KNOWN_SOURCE_TABLES,
            load_known_source_tables,
        )

        app_c = await build_app(settings)
        try:
            if not settings.has_credentials() and mode_l != "ogc":
                return {"status": "WARNING", "reason": "no credentials", "auth": "missing"}
            results: dict[str, object] = {
                "auth": "OK" if settings.has_credentials() else "missing",
                "catalog_mode": mode_l,
            }
            known = load_known_source_tables() or KNOWN_SOURCE_TABLES

            async def _probe_known() -> None:
                results["catalog_mode"] = "known" if mode_l == "known" else "known_tables_fallback"
                for sid, meta in known.items():
                    try:
                        rows = await query_table(
                            app_c.dgl,
                            service=meta["service"],
                            schema_table=meta["table_name"],
                            maxfeatures=1,
                        )
                        await app_c.source_registry.set_status(
                            sid, "OK", table_name=meta["table_name"]
                        )
                        results[sid] = {
                            "status": "OK",
                            "table": meta["table_name"],
                            "sample_rows": len(rows),
                        }
                    except Exception as exc:
                        err = redact_string(str(exc))
                        st = "ERROR"
                        if "403" in err or "Forbidden" in err:
                            st = "UNRESOLVED"
                        results[sid] = {
                            "status": st,
                            "table": meta["table_name"],
                            "error": err,
                        }

            async def _probe_ogc() -> None:
                from grand_lyon_mcp.providers.datagrandlyon.ogc_features import list_collections

                results["catalog_mode"] = (
                    "ogc" if mode_l == "ogc" else results.get("catalog_mode", "ogc")
                )
                try:
                    cols = await list_collections(app_c.dgl)
                except Exception as exc:
                    results["ogc"] = {
                        "status": "ERROR",
                        "error": redact_string(str(exc)),
                    }
                    return
                results["ogc"] = {"status": "OK", "collections": len(cols)}
                # Match known parking / keywords for candidates
                keywords = ("parking", "velov", "toilet", "dechet", "chantier")
                candidates = []
                for c in cols:
                    cid = str(c.get("id") or c.get("name") or "").lower()
                    title = str(c.get("title") or "").lower()
                    blob = f"{cid} {title}"
                    if any(k in blob for k in keywords):
                        candidates.append(cid or title)
                results["ogc_candidates"] = candidates[:30]

            catalog_usable = False
            catalog_forbidden = False

            if mode_l in {"auto", "full"}:
                for service in ("rdata", "grandlyon"):
                    try:
                        entries = await fetch_catalog(app_c.dgl, service)
                        results[service] = {"count": len(entries)}
                        catalog_usable = True
                        sources = await app_c.source_registry.list_all()
                        for src in sources:
                            meta = json.loads(src.get("metadata_json") or "{}")
                            kws = meta.get("search_keywords") or []
                            hints = meta.get("exact_table_hints") or []
                            cands = search_catalog(entries, kws, exact_table_hints=hints)
                            status = await app_c.source_registry.resolve_from_candidates(
                                src["source_id"], cands[:5], exact_hints=hints
                            )
                            results[src["source_id"]] = {
                                "status": status,
                                "candidates": len(cands),
                            }
                    except Exception as exc:
                        err = redact_string(str(exc))
                        st = "ERROR"
                        if "403" in err or "Forbidden" in err or "pas la permission" in err.lower():
                            st = "FORBIDDEN"
                            catalog_forbidden = True
                        results[service] = {"status": st, "error": err}
                if catalog_usable:
                    results["catalog_list"] = "OK"
                elif catalog_forbidden:
                    results["catalog_list"] = "FORBIDDEN"
                    results["note"] = "403 catalogue n'est pas un échec d'auth — bascule known+ogc"

            if mode_l == "known" or (mode_l == "auto" and not catalog_usable):
                await _probe_known()
            if mode_l == "ogc" or (mode_l == "auto" and not catalog_usable):
                await _probe_ogc()
            if mode_l == "full" and not catalog_usable:
                results["status"] = "PARTIAL"
                results["note"] = "full mode failed (often 403); try --mode auto|known|ogc"

            return results
        finally:
            await app_c.aclose()

    typer.echo(json.dumps(_run(_go()), indent=2, default=str))


@catalog_app.command("validate")
def catalog_validate() -> None:
    """Valide les sources résolues avec une requête bornée."""
    settings = get_settings()
    if settings.offline:
        typer.echo(json.dumps({"status": "DISABLED", "reason": "offline mode"}))
        raise typer.Exit(0)

    async def _go() -> dict[str, object]:
        from grand_lyon_mcp.bootstrap import build_app
        from grand_lyon_mcp.providers.datagrandlyon.datapusher import query_table

        app_c = await build_app(settings)
        try:
            out: dict[str, object] = {}
            for src in await app_c.source_registry.list_all():
                if src.get("status") != "OK" or not src.get("table_name"):
                    out[src["source_id"]] = {"status": src.get("status") or "UNRESOLVED"}
                    continue
                try:
                    rows = await query_table(
                        app_c.dgl,
                        service=src.get("service") or "rdata",
                        schema_table=src["table_name"],
                        maxfeatures=2,
                    )
                    out[src["source_id"]] = {"status": "OK", "sample_rows": len(rows)}
                except Exception as exc:
                    out[src["source_id"]] = {
                        "status": "ERROR",
                        "error": redact_string(str(exc)),
                    }
            return out
        finally:
            await app_c.aclose()

    typer.echo(json.dumps(_run(_go()), indent=2, default=str))


@sync_app.command("gtfs")
def sync_gtfs(
    zip_path: Path | None = typer.Option(
        None, "--from-file", help="Importer un ZIP local (offline)"
    ),
) -> None:
    """Télécharge / importe le GTFS TCL."""
    settings = get_settings()

    async def _go() -> dict[str, object]:
        from grand_lyon_mcp.bootstrap import build_app
        from grand_lyon_mcp.providers.gtfs.downloader import download_gtfs
        from grand_lyon_mcp.providers.gtfs.importer import import_gtfs_zip

        app_c = await build_app(settings)
        try:
            dest = settings.resolved_data_dir() / "gtfs" / "GTFS_TCL.ZIP"
            path: Path | None
            if zip_path is not None:
                path = zip_path
            elif settings.offline:
                return {"status": "DISABLED", "reason": "offline and no --from-file"}
            else:
                path, _, _downloaded = await download_gtfs(
                    app_c.http,
                    dest,
                    auth=app_c.dgl._auth,
                )
                if path is None:
                    return {"status": "OK", "downloaded": False, "message": "not modified"}
            assert path is not None
            counts = await import_gtfs_zip(app_c.conn, path)
            # index stops as entities (cap large GTFS)
            cursor = await app_c.conn.execute(
                "SELECT stop_id, stop_name, stop_lat, stop_lon FROM gtfs_stops LIMIT 20000"
            )
            rows = list(await cursor.fetchall())
            for r in rows:
                await app_c.entities.upsert(
                    logical_id=f"gtfs:stop:{r['stop_id']}",
                    source_id="gtfs_tcl",
                    provider_id=r["stop_id"],
                    entity_type="transport_stop",
                    name=r["stop_name"],
                    latitude=float(r["stop_lat"]),
                    longitude=float(r["stop_lon"]),
                )
            return {"status": "OK", "counts": counts, "entities_indexed": len(rows)}
        finally:
            await app_c.aclose()

    typer.echo(json.dumps(_run(_go()), indent=2, default=str))


@sync_app.command("static")
def sync_static() -> None:
    """Synchronise les jeux statiques (équipements) — stub offline-friendly."""
    settings = get_settings()
    if settings.offline:
        typer.echo(json.dumps({"status": "OK", "mode": "offline", "message": "skipped network"}))
        return
    typer.echo(json.dumps({"status": "OK", "message": "use catalog scan + validate first"}))


@snapshot_app.command("velov")
def snapshot_velov() -> None:
    """Enregistre un snapshot des stations Vélo'v."""
    settings = get_settings()

    async def _go() -> dict[str, object]:
        from grand_lyon_mcp.bootstrap import build_app

        app_c = await build_app(settings)
        try:
            # Lyon center
            env = await app_c.velov.stations_near_ref(
                __import__("grand_lyon_mcp.domain.common", fromlist=["PlaceRef"]).PlaceRef(
                    latitude=45.76, longitude=4.85
                ),
                radius_m=5000,
                limit=50,
            )
            return {"status": env.status.value, "count": len(env.data.get("stations") or [])}
        finally:
            await app_c.aclose()

    typer.echo(json.dumps(_run(_go()), indent=2, default=str))


@app.command("setup")
def setup_cmd(
    yes: bool = typer.Option(
        False,
        "--yes",
        "-y",
        help="Mode non interactif (valeurs par défaut / offline sauf --live)",
    ),
    offline: bool | None = typer.Option(
        None,
        "--offline/--live",
        help="Forcer le mode offline ou live (défaut: demandé, ou offline avec -y)",
    ),
    force_config: bool = typer.Option(
        False,
        "--force-config",
        help="Écraser les fichiers YAML de configuration existants",
    ),
    skip_install: bool = typer.Option(False, "--skip-install", help="Ne pas lancer uv sync"),
    skip_migrate: bool = typer.Option(False, "--skip-migrate", help="Ne pas migrer la base"),
) -> None:
    """Assistant TUI de configuration (secrets, config, install, migrate, doctor)."""
    from grand_lyon_mcp.setup_wizard import run_setup

    code = run_setup(
        non_interactive=yes,
        offline=offline if offline is not None else (True if yes else None),
        force_config=force_config,
        skip_install=skip_install,
        skip_migrate=skip_migrate,
    )
    raise typer.Exit(code)


@app.command("env")
def env_cmd(
    edit: bool = typer.Option(
        False,
        "--edit",
        help="Ouvrir secrets.env dans $EDITOR",
    ),
    config_dir: bool = typer.Option(
        False,
        "--config-dir",
        help="Afficher uniquement le répertoire de configuration résolu (utile pour les scripts)",
    ),
) -> None:
    """Affiche l'état de la configuration (valeurs sensibles masquées)."""
    from grand_lyon_mcp.setup_wizard import CONFIG_DIR_DEFAULT, SECRETS_NAME, show_env_status

    status = show_env_status()
    if config_dir:
        typer.echo(status["config_dir"])
        return
    typer.echo(json.dumps(status, indent=2, ensure_ascii=False))
    if edit:
        secrets = Path(status["config_dir"]) / SECRETS_NAME
        if not secrets.is_file():
            typer.echo(
                f"Fichier absent : {secrets}. Lancez d'abord `grand-lyon-mcp setup`.",
                err=True,
            )
            raise typer.Exit(1)
        editor = __import__("os").environ.get("EDITOR", "nano")
        raise typer.Exit(__import__("subprocess").call([editor, str(secrets)]))
    _ = CONFIG_DIR_DEFAULT


@app.command("client-config")
def client_config_cmd(
    wrapper: bool = typer.Option(
        True,
        "--wrapper/--bin",
        help="Utiliser scripts/run_mcp.sh (recommandé) ou le binaire venv",
    ),
) -> None:
    """Affiche un bloc `mcpServers` JSON prêt à coller (Claude Desktop, Cursor…)."""
    from grand_lyon_mcp.setup_wizard import client_config_snippet

    root = Path(__file__).resolve().parents[2]
    if wrapper:
        command = str(root / "scripts" / "run_mcp.sh")
    else:
        command = str(root / ".venv" / "bin" / "grand-lyon-mcp")
    typer.echo(client_config_snippet(command=command))


@app.command("smoke")
def smoke_cmd(
    tool: str | None = typer.Option(
        None,
        "--tool",
        help="N'exercer qu'un outil (ex: lyon_resolve_place). Défaut: tous.",
    ),
) -> None:
    """Teste les outils MCP en local (sans client MCP / sans stdio).

    Affiche le status de chaque appel. Utile pour vérifier que l'install marche
    après `make setup` — contrairement à `serve`, cette commande parle à l'humain.
    """
    from grand_lyon_mcp.adapters.mcp.tools import PUBLIC_TOOL_NAMES, dispatch_tool
    from grand_lyon_mcp.bootstrap import build_app

    settings = get_settings()
    setup_logging(settings.log_level)

    payloads: dict[str, dict[str, object]] = {
        "lyon_resolve_place": {"query": "Part-Dieu", "limit": 3},
        "lyon_next_departures": {"stop": {"query": "Bellecour"}, "line": "A", "limit": 3},
        "lyon_mobility_status": {"lines": ["A"], "include": ["transit", "traffic"]},
        "lyon_trip_options": {
            "origin": {"profile_place": "home"},
            "destination": {"profile_place": "work"},
            "modes": ["walk", "velov", "tcl"],
        },
        "lyon_parking_options": {"destination": {"query": "Hôtel de Ville"}, "limit": 3},
        "lyon_accessibility_check": {
            "origin": {"query": "Bellecour"},
            "destination": {"query": "Part-Dieu"},
            "needs": ["wheelchair"],
        },
        "lyon_nearby_facilities": {
            "location": {"latitude": 45.777, "longitude": 4.855},
            "categories": ["toilet", "park"],
            "radius_m": 2000,
        },
        "lyon_environment_brief": {
            "location": {"query": "Lyon"},
            "indicators": ["pollen", "heat"],
        },
        "lyon_waste_dropoff": {
            "item": "batterie de vélo électrique",
            "location": {"profile_place": "home"},
        },
        "lyon_personal_briefing": {"profile": "weekday_morning"},
    }

    names = [tool] if tool else list(PUBLIC_TOOL_NAMES)
    unknown = [n for n in names if n not in PUBLIC_TOOL_NAMES]
    if unknown:
        typer.echo(f"Outil(s) inconnu(s): {unknown}", err=True)
        raise typer.Exit(2)

    async def _go() -> list[tuple[str, str, str]]:
        app_c = await build_app(settings)
        rows: list[tuple[str, str, str]] = []
        try:
            for name in names:
                result = await dispatch_tool(app_c, name, payloads[name])
                status = str(result.get("status", "?"))
                summary = str(result.get("summary", ""))[:80]
                rows.append((name, status, summary))
        finally:
            await app_c.aclose()
        return rows

    typer.echo(
        f"grand-lyon-mcp smoke — mode={'offline' if settings.offline else 'live'} "
        f"db={settings.resolved_db_path()}"
    )
    typer.echo("")
    results = _run(_go())
    assert isinstance(results, list)
    ok = 0
    for name, status, summary in results:
        mark = "✓" if status in {"ok", "partial", "ambiguous", "not_found"} else "✗"
        if mark == "✓":
            ok += 1
        typer.echo(f"  {mark} {name:28} {status:14} {summary}")
    typer.echo("")
    typer.echo(f"{ok}/{len(results)} outils ont répondu avec une enveloppe valide.")
    if ok < len(results):
        raise typer.Exit(1)


def main() -> None:
    app()


if __name__ == "__main__":
    main()
