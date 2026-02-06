# head

Stop the pipeline after a fixed number of messages.

## Synopsis

```
bgpipe [...] -- head [OPTIONS]
```

## Description

The **head** stage lets only the first *N* messages pass and then stops the
pipeline. This is useful for sampling or quick tests against a live source.

The message count is taken from messages seen in the stage direction (or both
when using `-LR`). When the limit is reached, the stage passes that message and
stops the pipeline.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `-n`, `--count` | int | `10` | Number of messages to pass before stopping |

## Examples

Stop after 20 updates from a live session:

```bash
bgpipe \
    -- connect 192.0.2.1 \
    -- head -n 20 \
    -- stdout
```

Sample 100 messages in both directions:

```bash
bgpipe \
    -- listen :179 \
    -- head -LR -n 100 \
    -- connect 192.0.2.1
```

## See Also

[grep](grep.md),
[limit](limit.md),
[Stages overview](index.md)
