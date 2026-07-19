"""Accès aux données empaquetées (config + fixtures) via importlib.resources.

Fonctionne depuis un checkout comme depuis un wheel installé. On expose des
``pathlib.Path`` réels (les données sont livrées sur disque, jamais dans un zip),
ce qui reste compatible avec les vérifications ``is_file()`` du reste du code.
"""

from __future__ import annotations

from importlib import resources
from pathlib import Path


def data_dir() -> Path:
    """Répertoire racine des données empaquetées (``grand_lyon_mcp/_data``)."""
    return Path(str(resources.files("grand_lyon_mcp").joinpath("_data")))


def config_dir() -> Path:
    """Répertoire des templates de configuration empaquetés."""
    return data_dir() / "config"


def config_path(name: str) -> Path:
    """Chemin vers un template de configuration empaqueté (ex. ``sources.example.yaml``)."""
    return config_dir() / name


def fixtures_dir() -> Path:
    """Répertoire des fixtures de démonstration offline empaquetées."""
    return data_dir() / "fixtures"
