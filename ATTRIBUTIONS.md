# Attributions

The code uses the [MIT license](LICENSE). Provider data has separate licenses and
attribution requirements. Read the metadata for each dataset before redistribution.

This project is independent of Métropole de Lyon, SYTRAL Mobilités, Keolis-TCL,
and JCDecaux. Names and marks identify their services.

## Data producers

| Data | Producer or service | License reference |
| --- | --- | --- |
| Parking, road events, facilities, waste | Métropole de Lyon | Dataset metadata at [DataGrandLyon](https://data.grandlyon.com/) |
| TCL GTFS, departures, SIRI, accessibility | SYTRAL Mobilités | Dataset-specific portal terms |
| Vélo'v availability | JCDecaux through Métropole de Lyon | Dataset-specific portal terms |
| Geocoding | Métropole's [Photon](https://github.com/komoot/photon) instance | Photon: Apache-2.0; OSM-derived data: [OpenStreetMap copyright and ODbL](https://www.openstreetmap.org/copyright) |
| Optional journey planning | [Transitous](https://transitous.org/) and source GTFS feeds | Service terms and individual feed licenses |

Many Métropole datasets use the Etalab Open Licence. Do not assume that license
applies to every feed. Attribute OpenStreetMap-derived data to
“© OpenStreetMap contributors”.

The package contains demonstration and regression fixtures, not a complete live
dataset. Provenance fields are available in tool envelopes; not every provider
currently fills every provenance field. See [Data sources](docs/data-sources.md).

Photon and Transitous are shared services. Follow their usage terms and use an
appropriate dedicated service for high request volumes. A custom endpoint must
also satisfy this project's network allowlist.

## Software

Runtime dependencies are listed in `pyproject.toml`; exact versions are in
`uv.lock`. They include MCP, HTTPX, Pydantic, aiosqlite, PyYAML, Typer, Tenacity,
platformdirs, RapidFuzz, Shapely, pyproj, and python-dateutil.

Check the installed distributions for their license texts and bundled-library
notices. Shapely wheels can include GEOS (LGPL-2.1), and pyproj wheels can include
PROJ. A package's own license does not replace its bundled-library obligations.
