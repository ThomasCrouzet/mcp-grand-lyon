# Attributions

The code uses the [MIT license](LICENSE).
Provider data has separate licenses and attribution requirements.
Read each dataset's metadata before redistribution.

This project is independent of Métropole de Lyon, SYTRAL Mobilités, Keolis-TCL, and JCDecaux.
Names and marks identify their services.

## Data producers

| Data | Producer or service | License reference |
| --- | --- | --- |
| Parking, road events, facilities, waste | Métropole de Lyon | Dataset metadata at [DataGrandLyon](https://data.grandlyon.com/) |
| TCL GTFS, departures, SIRI, accessibility | SYTRAL Mobilités | Dataset-specific portal terms |
| Vélo'v availability | JCDecaux through Métropole de Lyon | Dataset-specific portal terms |
| Geocoding | Métropole's [Photon](https://github.com/komoot/photon) instance | Photon uses Apache-2.0; derived OSM data uses [ODbL](https://www.openstreetmap.org/copyright) |
| Optional journey planning | [Transitous](https://transitous.org/) and source GTFS feeds | Service terms and individual feed licenses |

Many Métropole datasets use the Etalab Open Licence.
Do not assume that this license applies to every feed.
Attribute OpenStreetMap-derived data to “© OpenStreetMap contributors”.

The binary contains demonstration and regression fixtures.
It does not contain a complete live dataset.
Tool envelopes can include provider, dataset, license, attribution, and timestamp fields.
Not every provider supplies every provenance field.

Photon and Transitous are shared services.
Follow their usage terms.
Use an appropriate dedicated service for high request volumes.

## Software

`go.mod` lists direct and indirect Go modules.
`go.sum` records module checksums.
Direct runtime modules include the MCP Go SDK, YAML v3, Unicode text support, and pure-Go SQLite.

Release archives include this file and `THIRD_PARTY_NOTICES.txt`.
The notice file contains exact texts from compiled modules and the Go toolchain.
The release script checks all supported operating system and architecture targets.

`scripts/build-release.sh` runs `go run ./scripts/notices` for each release.
A module's license does not replace the license obligations of bundled data.
