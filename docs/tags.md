# Message Tags

Tags are key-value metadata attached to messages: where a message came from
(eg. the BGP peer and the route collector), and what stages found out about it
(eg. its RPKI status). They travel with the message through the pipeline, are
written as the last element of the [JSON format](json-format.md), and can be
matched in [filters](filters.md#tags) with `tag[KEY]`.

## Conventions

- **ALL-CAPS keys** are the well-known tags below. They describe the source of
  a message, are set by the stages that read it, and are used by the stages
  that write it (eg. the MRT and BMP writers put `PEER_IP` and `PEER_AS` into
  their headers).
- **Lowercase keys** are all other tags, by convention `stage/name` for tags
  set by a stage (eg. `rov/status`), and anything else for your own tags.
- An empty value means no tag.

Internally, the well-known tags are kept as typed values (eg. `PEER_IP` as an
IP address), so they are cheap to set and use, and are always valid: an
invalid value (eg. `PEER_AS` of `abc`) is dropped.

## Well-known Tags

| Tag | Description | Set by |
|-----|-------------|--------|
| `PEER_IP` | IP address of the BGP peer | [read](stages/read.md) (MRT), [bmp](stages/bmp.md), [ris-live](stages/ris-live.md), [rv-live](stages/rv-live.md) |
| `PEER_AS` | AS number of the BGP peer | read (MRT), bmp, ris-live, rv-live |
| `PEER_ID` | BGP Identifier of the peer | bmp |
| `LOCAL_IP` | Local (collector) IP address of the BGP session | read (MRT update dumps) |
| `LOCAL_AS` | Local (collector) AS number of the BGP session | read (MRT update dumps) |
| `COLLECTOR` | Route collector name (eg. `rrc01`, `linx`) | ris-live, rv-live, bmp (OpenBMP) |
| `ROUTER` | BMP router: its sysName, or IP address | bmp |
| `SRC` | Name of the stage the message came from | [tag --src](stages/tag.md) |

[combine](stages/combine.md) and [mrai](stages/mrai.md) identify a BGP peer by
`COLLECTOR`, `PEER_IP` and `PEER_AS`.

## Stage Tags

| Tag | Description | Set by |
|-----|-------------|--------|
| `rov/status`, `rov/<prefix>` | RPKI ROV result: `VALID`, `INVALID`, `NOT_FOUND` (eg. `rov/192.0.2.0/24`) | [rov](stages/rov.md#tags) |
| `aspa/status`, `aspa/invalid-hop` | ASPA verification result | [aspa](stages/aspa.md#tags) |
| `bmp/rib` | RIB view: `in-post`, `out-pre`, `out-post`, or `loc` (not set for pre-policy Adj-RIB-In) | [bmp](stages/bmp.md) |
| `bmp/rd` | Peer distinguisher (hex), for RD and local instance peers, or if non-zero | bmp |
| `bmp/type` | `rd` or `local` for RD and local instance peers (not set for global peers) | bmp |
| `mrt/interface` | Interface index, when present | read (MRT update dumps) |
| `ris-live/host` | RIS collector hostname (eg. `rrc01.ripe.net`) | ris-live |
| `ris-live/id` | RIS message ID | ris-live |
| `rv-live/collector` | RouteViews internal BMP machine name (eg. `bmp1`) | rv-live |
| `websocket/remote` | Remote address of the WebSocket peer | [websocket](stages/websocket.md) |

## Using Tags

Match tags in [filters](filters.md#tags), eg. in [grep](stages/grep.md) or the
`--if` option of any stage:

```bash
bgpipe -o -- ris-live -- grep 'tag[PEER_AS] == 13335 && tag[COLLECTOR] == rrc01'
bgpipe -- bmp --listen :11019 -- grep 'tag[bmp/rib] == in-post' -- write 'bmp.$TIME.mrt.gz'
```

Add or drop tags with the [tag](stages/tag.md) stage, eg. to mark messages
from a given source, or to drop tags that should not be stored:

```bash
bgpipe -- read updates.mrt -- tag --add source=rv2 --drop mrt/interface -- write updates.json
```

When reading JSON, tags are read back from the last element of each message;
`write --format mrt` and `bmp --write` use the well-known tags to fill in the
peer headers.

## See Also

[JSON format](json-format.md),
[Message Filters](filters.md),
[tag](stages/tag.md)
