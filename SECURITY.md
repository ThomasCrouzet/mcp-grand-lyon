# Security Policy

## Signaler une vulnérabilité (FR)

Merci de **ne pas ouvrir d’issue publique** pour une faille de sécurité.

Utilisez le canal privé de GitHub : onglet **Security → Report a vulnerability**
([Private Vulnerability Reporting](https://github.com/ThomasCrouzet/mcp-grand-lyon/security/advisories/new))
du dépôt. Vous y décrivez la faille et un avis de sécurité privé est créé.

Délai de réponse indicatif : sous ~7 jours. Merci d’inclure une description, les étapes
de reproduction et l’impact estimé.

### Versions supportées

| Version | Supportée |
|---------|-----------|
| 0.1.x   | ✅ |

## Garanties et limites

- **Credentials** : uniquement via variables d’environnement (`DATAGRANDLYON_USERNAME` / `DATAGRANDLYON_PASSWORD`) ; jamais dans le dépôt, les logs, les erreurs, les fixtures ou les sorties MCP. HTTP Basic uniquement sur HTTPS via `httpx` (identifiants en en-tête, jamais dans l’URL).
- **Réseau** : allowlist de hosts appliquée à chaque requête sortante, y compris les cibles de redirections 3xx (anti-SSRF). Redaction systématique (`Authorization`, mots de passe, tokens…) dans les logs et exceptions.
- **Surface MCP** : outils en lecture seule uniquement ; aucun outil n’accepte d’URL, de SQL/CQL ni de filtre brut DataGrandLyon. Entrées validées par Pydantic (`extra="forbid"`, bornes strictes).
- **Hors garantie** : désactiver la vérification TLS (`GRAND_LYON_MCP_VERIFY_TLS=false` / `network.verify_tls: false`) expose les identifiants à une interception (MITM) et n’est pas couvert. Fournir une source (`TRANSITOUS_BASE_URL`) ou un fichier GTFS (`--from-file`) hors des hosts/sources de confiance sort du modèle de menace.

## Reporting a vulnerability (EN)

Please do **not** open a public issue for security problems. Use GitHub **Private
Vulnerability Reporting** (Security → Report a vulnerability). Expect a response within
~7 days. Credentials are environment-only; MCP tools are read-only; a host allowlist and
log redaction are enforced. Disabling TLS verification is out of the supported scope.
