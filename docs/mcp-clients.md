# Brancher un client MCP

`grand-lyon-mcp` expose le protocole **MCP** en transport **stdio**. Il fonctionne avec n’importe quel client MCP compatible (Claude Desktop, Cursor, agents maison…).

## Français

### Bloc de configuration prêt à coller

Générez un bloc `mcpServers` avec les chemins absolus de votre machine :

```bash
make client-config
# ou
uv run grand-lyon-mcp client-config          # via scripts/run_mcp.sh (recommandé)
uv run grand-lyon-mcp client-config --bin     # via le binaire du venv
```

Exemple de sortie (format standard `mcpServers`, à coller dans la config de votre client) :

```json
{
  "mcpServers": {
    "grand-lyon": {
      "command": "/chemin/absolu/mcp-grand-lyon/scripts/run_mcp.sh",
      "args": ["serve", "--transport", "stdio"]
    }
  }
}
```

- **Claude Desktop** : `claude_desktop_config.json` (voir la doc Claude Desktop pour l’emplacement selon l’OS).
- **Cursor** et autres clients : même bloc `mcpServers`.

### Secrets

Ne placez **jamais** d’identifiant dans la configuration du client MCP.

- **Option A — env hérité** : exportez `DATAGRANDLYON_USERNAME` et `DATAGRANDLYON_PASSWORD` dans l’environnement qui lance le client MCP ; le serveur en hérite.
- **Option B — wrapper local** : pointez `command` vers `scripts/run_mcp.sh` (chemin absolu). Le wrapper source `secrets.env` (`chmod 600`) hors dépôt et, sans identifiants, bascule automatiquement en mode offline (fixtures).

### Vérifications utiles

- Rechargez les serveurs MCP côté client.
- Listez les outils : seuls les `lyon_*` doivent apparaître.
- Test local sans client : `make smoke` (ou `grand-lyon-mcp smoke`).

### Mode offline

`GRAND_LYON_MCP_OFFLINE=true` : fixtures locales, aucun appel réseau. Utile pour valider la configuration d’un client sans credentials.

---

## English

`grand-lyon-mcp` speaks MCP over **stdio** and works with any compatible client.

### Ready-to-paste config

```bash
make client-config          # or: uv run grand-lyon-mcp client-config
```

```json
{
  "mcpServers": {
    "grand-lyon": {
      "command": "/absolute/path/mcp-grand-lyon/scripts/run_mcp.sh",
      "args": ["serve", "--transport", "stdio"]
    }
  }
}
```

### Secrets

Never put credentials in the client config.

- **Option A**: inherit `DATAGRANDLYON_USERNAME` / `DATAGRANDLYON_PASSWORD` from the parent process.
- **Option B**: `scripts/run_mcp.sh` sourcing `secrets.env` (`chmod 600`) — falls back to offline mode without credentials.

Logs go to stderr; stdout is MCP-only.
