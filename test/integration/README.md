# bgpipe integration tests

End-to-end tests running the real bgpipe binary against real BGP daemons in
docker containers: GoBGP, BIRD, FRR (BGP sessions in both directions), and
StayRTR (RPKI ROV over the RTR protocol).

Each test is a standalone shell script that doubles as a usage example: it
shows the exact docker and bgpipe commands needed to wire bgpipe to a given
daemon.

## Requirements

- docker (Docker Desktop, OrbStack, colima, or native Linux)
- jq
- go (to build bgpipe; or set BGPIPE_BIN to an existing binary)

## Usage

```
./run.sh                     # run the full suite
./run.sh gobgp               # run only tests matching "gobgp"
bash -x 20-connect-gobgp.sh  # debug a single test verbatim
BGPIPE_BIN=$(which bgpipe) ./run.sh   # test an installed build
```

On failure, a test dumps the daemon's docker logs, the bgpipe log, and the
bgpipe JSON output. Tests exit 77 to report SKIP (eg. no docker available);
in CI (env CI set) missing docker is a failure instead.

## Layout

- `run.sh` - test runner; builds bgpipe once into `.cache/`
- `lib.sh` - shared helpers: containers, readiness waits, JSON assertions
- `NN-*.sh` - one scenario per file, run in order
- `testdata/` - daemon configs and small fixtures; `*.in` files are templates
  rendered with the host IP and port at runtime

## Notes

- All containers are labeled and force-removed on exit, even on failure.
- Work dirs live under `.cache/` (not /tmp): docker VMs on macOS do not
  share /tmp, so bind mounts from /tmp would appear empty.
- The mapped host port is not a reliable readiness probe (docker/colima
  forwarders accept or refuse on their own schedule); tests wait for the
  daemon itself (eg. its listener via netstat, or a CLI probe) instead.
- StayRTR has no ASPA support; testing ASPA over RTR needs another server
  (eg. Routinator with real data) - a later phase.
