# read

Read messages from a file or URL.

## Synopsis

```
bgpipe [...] -- read [OPTIONS] PATH
```

## Description

The **read** stage reads BGP messages from a local file or a remote HTTP/HTTPS
URL and injects them into the pipeline. It supports bidirectional operation
with `-LR` and uses the data to set each message direction. Without `-LR`,
messages are injected in the stage direction.

The input format is auto-detected by default. Detection first tries the file
extension (e.g., `.mrt` or `.json`), then falls back to sampling the file
contents. Supported formats include JSON (one message per line), MRT (BGP4MP),
raw BGP wire format, ExaBGP line format, BMP (BGP Monitoring Protocol),
and OpenBMP.

Compressed files are decompressed automatically when `--decompress` is set
to `auto` (the default). The compression format is detected from the file
extension: `.gz` (gzip), `.bz2` (bzip2), `.zst` / `.zstd` (Zstandard).

For remote URLs, the stage streams data directly without downloading the
entire file first, making it suitable for large MRT archives.

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

Read a local MRT file and filter for a prefix:

```bash
bgpipe -o -- read updates.20240301.0000.bz2 -- grep 'prefix ~ 8.0.0.0/8'
```

Convert MRT to JSON:

```bash
bgpipe -- read updates.mrt.gz -- write output.json
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
[Stages overview](index.md)
