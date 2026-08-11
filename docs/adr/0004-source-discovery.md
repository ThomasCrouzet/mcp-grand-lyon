# ADR 0004: Découverte des sources DataGrandLyon

## Statut
Accepté (2026-07-19)

## Contexte
Les noms de tables évoluent ; il ne faut ni inventer ni choisir au hasard.

## Décision
Registre logique (`config/sources.yaml`) + résolution catalogue avec hints exacts ou candidat unique à score élevé. Ambiguïté → `UNRESOLVED`.

## Conséquences
Les outils MCP ne reçoivent jamais de noms de tables bruts ; `doctor` expose le statut par source.
