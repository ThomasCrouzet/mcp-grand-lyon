## Objectif

Que fait cette PR, et **pourquoi** ?

## Checklist

- [ ] `make quality` vert (ruff format + ruff check + mypy strict + pytest offline)
- [ ] Tests offline ajoutés/mis à jour (pas de dépendance réseau ni credentials)
- [ ] Aucun secret dans le code, les fixtures, les logs ou les sorties MCP
- [ ] Le domaine n’importe pas le SDK MCP / httpx / aiosqlite
- [ ] Décision non triviale documentée en ADR (`docs/adr/`) si nécessaire
- [ ] Documentation mise à jour si le comportement public change

## Notes

Contexte, captures, ou points d’attention pour la revue.
