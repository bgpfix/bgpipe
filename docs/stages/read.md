# read

Read messages from a file, URL, or standard input.

## Synopsis

```
bgpipe [...] -- read [OPTIONS] PATH
bgpipe [...] -- read [OPTIONS] -A PATH [PATH...]
```

## Description

The **read** stage reads BGP messages from a local file, a remote HTTP/HTTPS
URL, or standard input (use `-` as *PATH*) and injects them into the pipeline. It supports bidirectional operation
with `-LR` and uses the data to set each message direction. Without `-LR`,
messages are injected in the stage direction.

The input format is auto-detected by default. Detection first tries the file
extension (e.g., `.mrt` or `.json`), then falls back to sampling the file
contents. Supported formats include JSON (one message per line), MRT,
raw BGP wire format, ExaBGP line format, BMP (BGP Monitoring Protocol),
and OpenBMP.

For MRT, both BGP4MP update dumps (`updates.*`) and TABLE_DUMP /
TABLE_DUMP_V2 RIB snapshots (RouteViews `rib.*`, RIPE RIS `bview.*`) are
supported. RIB snapshots are converted into synthetic BGP UPDATE messages,
tagged with `PEER_AS` and `PEER_IP`. See [MRT files](../mrt.md) for details
and use cases.

Compressed files are decompressed automatically when `--decompress` is set
to `auto` (the default). The compression format is detected from the file
extension: `.gz` (gzip), `.bz2` (bzip2), `.zst` / `.zstd` (Zstandard).

For remote URLs, the stage streams data directly without downloading the
entire file first, making it suitable for large MRT archives.

With `-A`, the stage reads several paths (files or URLs, each possibly
compressed on its own) one after another, in the order given, as one
continuous stream. The data format is detected once, so all paths must hold
the same format; each path must be a complete file (the tail of a truncated
one, eg. still being written, garbles the start of the next one and stops the
stage with an error), and local files are checked for existence before
anything is read.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--decompress` | string | `auto` | Decompression: `auto`, `gz`, `bzip2`, `zstd`, or `none` |
| `--format` | string | `auto` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, `obmp`, or `auto` |
| `--type` | strings | | Process only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |
| `--pardon` | bool | `false` | Ignore input parsing errors |
| `--no-seq` | bool | `false` | Overwrite input sequence numbers |
| `--no-time` | bool | `false` | Overwrite input timestamps |
| `--no-tags` | bool | `false` | Drop input message tags |

## Examples

Read a compressed MRT file from the RIPE RIS archive:

```bash
bgpipe -o -- read https://data.ris.ripe.net/rrc01/latest-update.gz
```

Read a whole day of hourly dumps as one stream:

```bash
bgpipe -g -- read -A dumps/ris.20261025.*.json.zst -- rov -- aspa --role provider
```

Read a local MRT file and filter for a prefix:

```bash
bgpipe -o -- read updates.20240301.0000.bz2 -- grep 'prefix ~ 8.0.0.0/8'
```

Convert MRT to JSON:

```bash
bgpipe -- read updates.mrt.gz -- write output.json
```

Stream a RouteViews RIB snapshot as BGP UPDATEs:

```bash
bgpipe -- read rib.20240301.0000.bz2 -- write rib.json
```

Read a RIB snapshot under a non-standard name (content sampling cannot detect
table dumps, so the format must be given explicitly):

```bash
bgpipe -- read --format mrt my-snapshot.dat -- write rib.json
```

Replay an MRT file into a live BGP session after establishment:

```bash
bgpipe \
    -- speaker --active --asn 65001 \
    -- read --wait ESTABLISHED updates.mrt.zst \
    -- listen :179
```

## See Also

[write](write.md),
[stdin](stdin.md),
[MRT files](../mrt.md),
[Stages overview](index.md)
