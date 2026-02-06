# write

Write messages to a file.

## Synopsis

```
bgpipe [...] -- write [OPTIONS] PATH
```

## Description

The **write** stage writes BGP messages to a local file. It is a consumer that
supports bidirectional operation with `-LR` and mirrors messages flowing
through the pipeline, serializing them to disk without consuming them -
downstream stages still see every message.

The *PATH* argument supports time-based placeholders for automatic file rotation:

- `$TIME` is replaced with the current time formatted according to `--time-format`
- `${format}` uses a custom Go time format (e.g., `${2006-01-02}`)

When `--every` is set, the stage rotates to a new file at the given interval.
The minimum rotation interval is 60 seconds.

Files are written atomically: data is first written to a `.tmp` file, then
renamed to the final path on successful completion. Empty files are removed
automatically. Parent directories are created as needed.

The output format is auto-detected from the file extension by default.
For example, `output.json` selects JSON, `output.mrt` selects MRT.
Compression is also auto-detected: `.gz` (gzip), `.bz2` (bzip2),
`.zst` / `.zstd` (Zstandard).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `--format` | string | `auto` | Data format: `json`, `raw`, `mrt`, `exa`, `bmp`, `obmp`, or `auto` (detected from extension) |
| `--append` | bool | `false` | Append to file if it already exists |
| `--create` | bool | `false` | Fail if the file already exists |
| `--compress` | string | `auto` | Compression: `auto`, `gz`, `bzip2`, `zstd`, or `none` |
| `--every` | duration | `0` | Rotate to a new file at this interval (min 60s) |
| `--time-format` | string | `20060102.1504` | Go time format for `$TIME` placeholder |
| `--type` | strings | | Write only messages of given type(s) |
| `--skip` | strings | | Skip messages of given type(s) |

## Examples

Write a BGP session to a JSON file:

```bash
bgpipe -- connect 192.0.2.1 -- write -LR session.json -- connect 10.0.0.1
```

Write compressed MRT with hourly rotation:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- write -LR --every 1h 'updates.$TIME.mrt.gz' \
    -- connect 10.0.0.1
```

Archive only UPDATE messages:

```bash
bgpipe -- read input.mrt.gz -- write --type UPDATE updates.json
```

Append to an existing log:

```bash
bgpipe -- read new-data.mrt.gz -- write --append archive.json
```

## See Also

[read](read.md),
[stdout](stdout.md),
[Stages overview](index.md)
