"""Interactive setup wizard (TUI) for first-time configuration."""

from __future__ import annotations

import os
import re
import shutil
import stat
import subprocess
import sys
from pathlib import Path
from typing import Any

from platformdirs import user_config_dir, user_data_dir

APP_NAME = "grand-lyon-mcp"
CONFIG_DIR_DEFAULT = Path(user_config_dir(APP_NAME, APP_NAME))
DATA_DIR_DEFAULT = Path(user_data_dir(APP_NAME, APP_NAME))
SECRETS_NAME = "secrets.env"

ENV_KEYS = (
    "DATAGRANDLYON_USERNAME",
    "DATAGRANDLYON_PASSWORD",
    "GRAND_LYON_MCP_CONFIG_DIR",
    "GRAND_LYON_MCP_DATA_DIR",
    "GRAND_LYON_MCP_DB_PATH",
    "GRAND_LYON_MCP_LOG_LEVEL",
    "GRAND_LYON_MCP_OFFLINE",
    "GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS",
    "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS",
    "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS",
    "TRANSITOUS_ENABLED",
    "TRANSITOUS_BASE_URL",
)


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def _has_rich() -> bool:
    try:
        import rich  # noqa: F401

        return True
    except ImportError:
        return False


def _is_interactive() -> bool:
    return sys.stdin.isatty() and sys.stdout.isatty()


def _print(msg: str = "", *, err: bool = False) -> None:
    stream = sys.stderr if err else sys.stdout
    if _has_rich():
        from rich.console import Console

        Console(stderr=err).print(msg)
    else:
        print(msg, file=stream)


def _rule(title: str) -> None:
    if _has_rich():
        from rich.console import Console
        from rich.rule import Rule

        Console().print(Rule(title))
    else:
        print(f"\n=== {title} ===\n")


def _panel(msg: str) -> None:
    if _has_rich():
        from rich.console import Console
        from rich.panel import Panel

        Console().print(Panel.fit(msg, border_style="cyan"))
    else:
        print(f"\n{msg}\n")


def _run_gum(args: list[str]) -> str | None:
    """Run gum; UI on stderr (terminal), value on stdout only."""
    gum = shutil.which("gum")
    if not gum or not _is_interactive():
        return None
    try:
        # Important: do NOT capture stderr: gum draws the TUI there.
        result = subprocess.run(
            [gum, *args],
            check=False,
            stdout=subprocess.PIPE,
            stderr=None,
            text=True,
        )
        if result.returncode != 0:
            return None
        return result.stdout.rstrip("\n")
    except OSError:
        return None


def _prompt(label: str, default: str = "", *, password: bool = False) -> str:
    """Prompt using gum when available, else rich/getpass/input."""
    if shutil.which("gum") and _is_interactive():
        args = ["input", f"--prompt={label} ", f"--placeholder={default or '…'}"]
        if default and not password:
            args.append(f"--value={default}")
        if password:
            args.append("--password")
            if default:
                args.append("--placeholder=••••  (Entrée = conserver)")
        value = _run_gum(args)
        if value is not None:
            return value if value != "" else default

    if password:
        import getpass

        hint = " [Entrée = conserver]" if default else ""
        entered = getpass.getpass(f"{label}{hint}: ")
        return entered if entered else default

    if _has_rich() and _is_interactive():
        from rich.prompt import Prompt

        return str(Prompt.ask(label, default=default if default else None) or default)

    raw = input(f"{label} [{default}]: " if default else f"{label}: ")
    return raw.strip() if raw.strip() else default


def _confirm(label: str, default: bool = True) -> bool:
    if shutil.which("gum") and _is_interactive():
        args = ["confirm", label]
        # gum confirm: exit 0 = yes, 1 = no
        gum = shutil.which("gum")
        assert gum is not None
        try:
            cmd = [gum, *args]
            if not default:
                # gum 0.17 uses --default=no for default no
                cmd.append("--default=no")
            rc = subprocess.run(cmd, check=False, stderr=None).returncode
            if rc in (0, 1):
                return rc == 0
        except OSError:
            pass

    if _has_rich() and _is_interactive():
        from rich.prompt import Confirm

        return bool(Confirm.ask(label, default=default))

    yn = "O/n" if default else "o/N"
    raw = input(f"{label} [{yn}]: ").strip().lower()
    if not raw:
        return default
    return raw in {"y", "yes", "o", "oui"}


def _choose(label: str, options: list[str], default: str) -> str:
    if shutil.which("gum") and _is_interactive():
        # Prefer default first in list for visibility
        ordered = [default] + [o for o in options if o != default]
        value = _run_gum(["choose", "--header", label, *ordered])
        if value is not None and value in options:
            return value

    if _has_rich() and _is_interactive():
        from rich.prompt import Prompt

        return str(Prompt.ask(label, choices=options, default=default))

    print(label)
    for i, opt in enumerate(options, 1):
        mark = "*" if opt == default else " "
        print(f"  {i}. [{mark}] {opt}")
    raw = input(f"Choix [1-{len(options)}, défaut={options.index(default) + 1}]: ").strip()
    if raw.isdigit() and 1 <= int(raw) <= len(options):
        return options[int(raw) - 1]
    return default


def parse_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.is_file():
        return values
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        values[key.strip()] = val.strip().strip('"').strip("'")
    return values


def _shell_quote_env_value(value: str) -> str:
    """Quote value for POSIX `source` safety (spaces, etc.)."""
    if value == "":
        return ""
    escaped = (
        value.replace("\\", "\\\\").replace('"', '\\"').replace("`", "\\`").replace("$", "\\$")
    )
    return f'"{escaped}"'


def write_env_file(path: Path, values: dict[str, str], *, template: Path | None = None) -> None:
    """Write env file preserving comments from template when possible."""
    path.parent.mkdir(parents=True, exist_ok=True)
    lines: list[str] = []
    seen: set[str] = set()
    source_lines: list[str]
    if template and template.is_file():
        source_lines = template.read_text(encoding="utf-8").splitlines()
    else:
        source_lines = [f"{k}=" for k in ENV_KEYS]

    for line in source_lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            lines.append(line)
            continue
        key = stripped.split("=", 1)[0].strip()
        seen.add(key)
        if key in values:
            lines.append(f"{key}={_shell_quote_env_value(values[key])}")
        else:
            lines.append(line)

    for key, val in values.items():
        if key not in seen:
            lines.append(f"{key}={_shell_quote_env_value(val)}")

    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    path.chmod(stat.S_IRUSR | stat.S_IWUSR)  # 600


def copy_config_examples(
    config_dir: Path, *, force: bool = False, offline: bool | None = None
) -> list[str]:
    from grand_lyon_mcp.resources import config_dir as packaged_config_dir

    packaged = packaged_config_dir()
    mapping = {
        "settings.example.yaml": "settings.yaml",
        "sources.example.yaml": "sources.yaml",
        "profiles.example.yaml": "profiles.yaml",
        "waste-taxonomy.yaml": "waste-taxonomy.yaml",
    }
    written: list[str] = []
    config_dir.mkdir(parents=True, exist_ok=True)
    for src_name, dest_name in mapping.items():
        src = packaged / src_name
        dest = config_dir / dest_name
        if not src.is_file():
            continue
        if dest.exists() and not force:
            if dest_name == "settings.yaml" and offline is not None:
                _patch_settings_offline(dest, offline)
            continue
        shutil.copy2(src, dest)
        if dest_name == "settings.yaml" and offline is not None:
            _patch_settings_offline(dest, offline)
        written.append(dest_name)
    return written


def _patch_settings_offline(path: Path, offline: bool) -> None:
    """Set runtime.offline in settings.yaml without rewriting the whole file."""
    if not path.is_file():
        return
    text = path.read_text(encoding="utf-8")
    patched, n = re.subn(
        r"(?m)^(\s*offline:\s*)(true|false)\s*$",
        rf"\1{'true' if offline else 'false'}",
        text,
        count=1,
    )
    if n:
        path.write_text(patched, encoding="utf-8")


def client_config_snippet(*, command: str) -> str:
    """Bloc de configuration prêt à coller pour un client MCP stdio.

    Format ``mcpServers`` JSON standard, compatible Claude Desktop, Cursor et la
    plupart des clients MCP. `command` doit être un chemin absolu vers le binaire
    (ou le wrapper `scripts/run_mcp.sh`).
    """
    import json

    block = {
        "mcpServers": {
            "grand-lyon": {
                "command": command,
                "args": ["serve", "--transport", "stdio"],
            }
        }
    }
    return json.dumps(block, indent=2, ensure_ascii=False)


def run_setup(
    *,
    non_interactive: bool = False,
    offline: bool | None = None,
    force_config: bool = False,
    skip_install: bool = False,
    skip_migrate: bool = False,
) -> int:
    """Run the setup wizard. Returns process exit code."""
    repo = _repo_root()
    _rule("grand-lyon-mcp, assistant de configuration")
    _panel(
        "[bold]Bienvenue[/bold]\n\n"
        "Cet assistant va :\n"
        "  1. vous demander (optionnel) votre [bold]identifiant DataGrandLyon[/bold]\n"
        "  2. enregistrer les secrets hors du dépôt git\n"
        "  3. installer les dépendances et migrer la base\n\n"
        "Compte gratuit : [link=https://data.grandlyon.com]https://data.grandlyon.com[/link]\n"
        "Sans compte → mode offline (données de démo / fixtures)."
        if _has_rich()
        else "Compte data.grandlyon.com optionnel. Sans compte = mode offline."
    )

    if not _is_interactive() and not non_interactive:
        _print(
            "[yellow]Terminal non interactif détecté.[/yellow] "
            "Utilisez [bold]make setup-offline[/bold] ou "
            "[bold]grand-lyon-mcp setup --yes --live[/bold] avec les variables déjà exportées."
            if _has_rich()
            else "Terminal non interactif : utilisez setup --yes --offline ou --live."
        )
        if offline is None:
            offline = True
        non_interactive = True

    config_dir = Path(
        os.environ.get("GRAND_LYON_MCP_CONFIG_DIR") or CONFIG_DIR_DEFAULT
    ).expanduser()
    data_dir = Path(os.environ.get("GRAND_LYON_MCP_DATA_DIR") or DATA_DIR_DEFAULT).expanduser()
    secrets_path = config_dir / SECRETS_NAME
    existing = parse_env_file(secrets_path)

    if non_interactive:
        mode_offline = True if offline is None else offline
        values = _default_values(config_dir, data_dir, offline=mode_offline, existing=existing)
        # Pick up credentials already in the environment for --yes --live
        if not mode_offline:
            values["DATAGRANDLYON_USERNAME"] = (
                os.environ.get("DATAGRANDLYON_USERNAME")
                or existing.get("DATAGRANDLYON_USERNAME")
                or ""
            )
            values["DATAGRANDLYON_PASSWORD"] = (
                os.environ.get("DATAGRANDLYON_PASSWORD")
                or existing.get("DATAGRANDLYON_PASSWORD")
                or ""
            )
            if not values["DATAGRANDLYON_USERNAME"] or not values["DATAGRANDLYON_PASSWORD"]:
                _print(
                    "[red]Mode live sans identifiants.[/red] "
                    "Exportez DATAGRANDLYON_USERNAME et DATAGRANDLYON_PASSWORD "
                    "ou lancez [bold]make setup[/bold] en interactif."
                    if _has_rich()
                    else "Mode live sans identifiants dans l'environnement."
                )
                return 2
        return _apply(
            values,
            config_dir=config_dir,
            data_dir=data_dir,
            secrets_path=secrets_path,
            force_config=force_config,
            skip_install=skip_install,
            skip_migrate=skip_migrate,
            repo=repo,
        )

    # ── Interactive path ─────────────────────────────────────────────
    _rule("1 / 4; Compte DataGrandLyon")
    _print(
        "Pour les [bold]vraies[/bold] données (TCL, Vélo'v, parkings…), "
        "il faut le login / mot de passe du portail open data "
        "([link=https://data.grandlyon.com]data.grandlyon.com[/link]).\n"
        "Ce n'est [bold]pas[/bold] une clé API OpenAI."
        if _has_rich()
        else "Identifiants data.grandlyon.com (pas une clé API LLM)."
    )

    username = existing.get("DATAGRANDLYON_USERNAME", "") or os.environ.get(
        "DATAGRANDLYON_USERNAME", ""
    )
    password = existing.get("DATAGRANDLYON_PASSWORD", "") or os.environ.get(
        "DATAGRANDLYON_PASSWORD", ""
    )
    has_saved = bool(username and password)

    if offline is True:
        # Explicit --offline: skip credential prompts
        want_account = False
        mode_offline = True
        _print("Mode offline forcé (--offline) : pas d'identifiants demandés.")
    elif offline is False:
        want_account = True
        mode_offline = False
    else:
        want_account = _confirm(
            "Configurer un compte DataGrandLyon maintenant (identifiant + mot de passe) ?",
            default=not has_saved,  # if already configured, default no
        )
        mode_offline = not want_account

    if want_account:
        if has_saved:
            _print(
                f"Identifiant déjà enregistré : [cyan]{username}[/cyan]"
                if _has_rich()
                else f"Identifiant déjà enregistré : {username}"
            )
            if not _confirm("Conserver ces identifiants ?", default=True):
                username = ""
                password = ""

        if not username or not password or not has_saved:
            _print("")
            username = _prompt(
                "Identifiant DataGrandLyon (e-mail ou login)",
                username,
            )
            password = _prompt(
                "Mot de passe DataGrandLyon",
                password,
                password=True,
            )

        if username and password:
            mode_offline = False
            _print(
                "[green]✓ Identifiants pris en compte → mode live[/green]"
                if _has_rich()
                else "✓ Mode live"
            )
        else:
            mode_offline = True
            username = ""
            password = ""
            _print(
                "[yellow]Identifiants incomplets → bascule en mode offline "
                "(fixtures locales).[/yellow]"
                if _has_rich()
                else "Identifiants incomplets → offline."
            )
    else:
        username = ""
        password = ""
        mode_offline = True
        _print(
            "Mode [bold]offline[/bold] : fixtures locales, aucun appel réseau."
            if _has_rich()
            else "Mode offline."
        )

    _rule("2 / 4; Répertoires")
    config_dir = Path(_prompt("Répertoire de configuration", str(config_dir))).expanduser()
    data_dir = Path(_prompt("Répertoire de données", str(data_dir))).expanduser()
    secrets_path = config_dir / SECRETS_NAME
    # Re-read if user changed config dir
    existing = parse_env_file(secrets_path)

    _rule("3 / 4; Options")
    log_level = _choose(
        "Niveau de log",
        ["INFO", "DEBUG", "WARNING", "ERROR"],
        existing.get("GRAND_LYON_MCP_LOG_LEVEL", "INFO"),
    )
    transitous = _confirm(
        "Activer Transitous (itinéraires optionnels, hors TCL officiel) ?",
        default=existing.get("TRANSITOUS_ENABLED", "false").lower() == "true",
    )

    values = _default_values(config_dir, data_dir, offline=mode_offline, existing=existing)
    values["DATAGRANDLYON_USERNAME"] = username
    values["DATAGRANDLYON_PASSWORD"] = password
    values["GRAND_LYON_MCP_LOG_LEVEL"] = log_level
    values["TRANSITOUS_ENABLED"] = "true" if transitous else "false"
    values["GRAND_LYON_MCP_OFFLINE"] = "true" if mode_offline else "false"

    _rule("4 / 4; Confirmation")
    mode_label = "offline (fixtures)" if mode_offline else "live (DataGrandLyon)"
    summary = (
        f"  Mode          : [bold]{mode_label}[/bold]\n"
        f"  Identifiant   : {username if username else '(aucun)'}\n"
        f"  Mot de passe  : {'••••••••' if password else '(aucun)'}\n"
        f"  Config        : {config_dir}\n"
        f"  Données       : {data_dir}\n"
        f"  Secrets       : {secrets_path}\n"
        f"  Log           : {log_level}\n"
        f"  Transitous    : {'oui' if transitous else 'non'}"
    )
    plain = f"Mode={mode_label} user={username or '-'} cfg={config_dir}"
    _print(summary if _has_rich() else plain)

    if not _confirm("Écrire la configuration et finaliser ?", default=True):
        _print("Annulé.")
        return 1

    force = force_config or _confirm(
        "Écraser les fichiers YAML de config s'ils existent déjà ?",
        default=False,
    )
    return _apply(
        values,
        config_dir=config_dir,
        data_dir=data_dir,
        secrets_path=secrets_path,
        force_config=force,
        skip_install=skip_install,
        skip_migrate=skip_migrate,
        repo=repo,
    )


def _default_values(
    config_dir: Path,
    data_dir: Path,
    *,
    offline: bool,
    existing: dict[str, str],
) -> dict[str, str]:
    db_path = existing.get("GRAND_LYON_MCP_DB_PATH") or str(data_dir / "grand_lyon_mcp.db")
    return {
        "DATAGRANDLYON_USERNAME": existing.get("DATAGRANDLYON_USERNAME", ""),
        "DATAGRANDLYON_PASSWORD": existing.get("DATAGRANDLYON_PASSWORD", ""),
        "GRAND_LYON_MCP_CONFIG_DIR": str(config_dir),
        "GRAND_LYON_MCP_DATA_DIR": str(data_dir),
        "GRAND_LYON_MCP_DB_PATH": db_path,
        "GRAND_LYON_MCP_LOG_LEVEL": existing.get("GRAND_LYON_MCP_LOG_LEVEL", "INFO"),
        "GRAND_LYON_MCP_OFFLINE": "true" if offline else "false",
        "GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS": existing.get(
            "GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS", "5"
        ),
        "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS": existing.get(
            "GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS", "20"
        ),
        "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS": existing.get(
            "GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS", "6"
        ),
        "TRANSITOUS_ENABLED": existing.get("TRANSITOUS_ENABLED", "false"),
        "TRANSITOUS_BASE_URL": existing.get(
            "TRANSITOUS_BASE_URL", "https://api.transitous.org/api/"
        ),
    }


def _apply(
    values: dict[str, str],
    *,
    config_dir: Path,
    data_dir: Path,
    secrets_path: Path,
    force_config: bool,
    skip_install: bool,
    skip_migrate: bool,
    repo: Path,
) -> int:
    _rule("Application")
    data_dir.mkdir(parents=True, exist_ok=True)
    config_dir.mkdir(parents=True, exist_ok=True)

    template = repo / ".env.example"
    write_env_file(secrets_path, values, template=template if template.is_file() else None)
    _print(f"✓ Secrets écrits dans {secrets_path} (chmod 600)")

    offline_flag = values.get("GRAND_LYON_MCP_OFFLINE", "false").lower() == "true"
    written = copy_config_examples(config_dir, force=force_config, offline=offline_flag)
    if written:
        _print(f"✓ Config copiée : {', '.join(written)}")
    else:
        _print(f"✓ Config déjà présente dans {config_dir} (offline={offline_flag})")

    local_env = repo / ".env"
    # Always refresh local .env so make serve picks up the latest mode
    write_env_file(local_env, values, template=template if template.is_file() else None)
    _print(f"✓ Fichier local {local_env} (gitignored)")

    if not skip_install:
        _print("→ Installation des dépendances (uv sync)…")
        rc = subprocess.run(
            ["uv", "sync", "--all-extras", "--dev"],
            cwd=repo,
            check=False,
        ).returncode
        if rc != 0:
            _print("✗ uv sync a échoué", err=True)
            return rc

    env = os.environ.copy()
    env.update({k: v for k, v in values.items() if v != ""})

    if not skip_migrate:
        _print("→ Migrations SQLite…")
        rc = subprocess.run(
            ["uv", "run", "grand-lyon-mcp", "db", "migrate"],
            cwd=repo,
            env=env,
            check=False,
        ).returncode
        if rc != 0:
            _print("✗ db migrate a échoué", err=True)
            return rc

    _print("→ Diagnostic (doctor)…")
    subprocess.run(
        ["uv", "run", "grand-lyon-mcp", "doctor"],
        cwd=repo,
        env=env,
        check=False,
    )

    wrapper = repo / "scripts" / "run_mcp.sh"
    venv_bin = repo / ".venv" / "bin" / "grand-lyon-mcp"
    command = str(wrapper if wrapper.is_file() else venv_bin)

    _rule("Configuration client MCP")
    snippet = client_config_snippet(command=command)
    _print(snippet)
    snippet_out = config_dir / "mcp-client.snippet.json"
    snippet_out.write_text(snippet, encoding="utf-8")
    _print(f"✓ Snippet sauvé dans {snippet_out}")

    _rule("Prochaines étapes")
    if offline_flag:
        _print(
            "Mode offline prêt. Lancez :\n"
            "  [bold]make serve[/bold]\n"
            "ou collez le bloc ci-dessus dans la config de votre client MCP "
            "(Claude Desktop, Cursor…).\n\n"
            "Pour ajouter un compte plus tard : [bold]make setup[/bold] "
            "et répondez [bold]Oui[/bold] à la question DataGrandLyon."
            if _has_rich()
            else "Mode offline. make serve  |  plus tard: make setup"
        )
    else:
        _print(
            "Mode live. Commandes utiles :\n"
            "  [bold]make catalog-scan[/bold]\n"
            "  [bold]make catalog-validate[/bold]\n"
            "  [bold]make sync-gtfs[/bold]\n"
            "  [bold]make serve[/bold]"
            if _has_rich()
            else "Mode live: make catalog-scan && make sync-gtfs && make serve"
        )
    return 0


def show_env_status() -> dict[str, Any]:
    """Return a redacted status dict of current configuration."""
    config_dir = Path(
        os.environ.get("GRAND_LYON_MCP_CONFIG_DIR") or CONFIG_DIR_DEFAULT
    ).expanduser()
    secrets_path = config_dir / SECRETS_NAME
    file_vals = parse_env_file(secrets_path)
    merged = {**file_vals}
    for key in ENV_KEYS:
        if key in os.environ and os.environ[key] != "":
            merged[key] = os.environ[key]

    def mask(key: str, val: str) -> str:
        if not val:
            return "(vide)"
        if key in {"DATAGRANDLYON_PASSWORD"}:
            return "[set]"
        if key in {"DATAGRANDLYON_USERNAME"}:
            return val  # login is useful to see; not a secret key
        return val

    return {
        "config_dir": str(config_dir),
        "secrets_file": str(secrets_path),
        "secrets_exists": secrets_path.is_file(),
        "values": {k: mask(k, merged.get(k, "")) for k in ENV_KEYS},
    }
