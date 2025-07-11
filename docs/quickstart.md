## Installation

To get started with `bgpipe`, you need to install it on your system, ie. where you want it to proxy or terminate BGP sessions. `bgpipe` is a single binary that can be run on any machine with a compatible operating system (preferably Linux). It does not require any additional libraries or dependencies, making it easy to deploy - just copy the binary to your target machine.

You can download pre-built binaries from the [GitHub Releases page](https://github.com/bgpfix/bgpipe/releases/latest):

```bash
wget https://github.com/bgpfix/bgpipe/releases/latest/download/bgpipe-linux-amd64
chmod +x bgpipe-linux-amd64
sudo mv -f bgpipe-linux-amd64 /usr/local/bin/bgpipe   # note (1)
```

1. Make sure that the target directory is in your `$PATH`, or simply rename the binary to `bgpipe` and keep executing it from the current directory.

Alternatively, you can compile from source. You need to have [Go installed](https://go.dev/doc/install) first. Then, you can run:

```bash
go install github.com/bgpfix/bgpipe@latest   # note (1)
```

1. Make sure to put the resultant `bgpipe` binary in your `$PATH`. Go installs executables in the directory named by the `$GOBIN` environment variable, which defaults to `$GOPATH/bin`, or `$HOME/go/bin` if the `$GOPATH` variable is not set.

## Running bgpipe

When you run `bgpipe` without any arguments, it will print the help message, for example:

```
Usage: bgpipe [OPTIONS] [--] STAGE1 [OPTIONS] [ARGUMENTS] [--] STAGE2...

Options:
  -v, --version          print detailed version info and quit
  -n, --explain          print the pipeline as configured and quit
  -l, --log string       log level (debug/info/warn/error/disabled) (default "info")
      --pprof string     bind pprof to given listen address
  -e, --events strings   log given events ("all" means all events) (default [PARSE,ESTABLISHED,EOR])
  -k, --kill strings     kill session on any of these events
  -i, --stdin            read JSON from stdin
  -o, --stdout           write JSON to stdout
  -I, --stdin-wait       like --stdin but wait for EVENT_ESTABLISHED
  -O, --stdout-wait      like --stdout but wait for EVENT_EOR
  -2, --short-asn        use 2-byte ASN numbers
      --caps string      use given BGP capabilities (JSON format)

Supported stages (run <stage> -h to get its help)
  connect                connect to a BGP endpoint over TCP
  drop                   drop messages that match a filter
  exec                   handle messages in a background process
  grep                   drop messages that DO NOT match a filter
  limit                  limit prefix lengths and counts
  listen                 let a BGP client connect over TCP
  pipe                   process messages through a named pipe
  read                   read messages from file or URL
  speaker                run a simple BGP speaker
  stdin                  read messages from stdin
  stdout                 print messages to stdout
  tag                    add or drop message tags
  update                 modify UPDATE messages
  websocket              process messages over websocket
  write                  write messages to file
```

From the above output, you can learn the basic syntax of a *pipeline*, which is a sequence of *stages*. Usually the stages are separated by `--` characters; otherwise, `bgpipe` will try to separate the stages automatically, although this can lead to ambiguities for more complex pipelines. Global `bgpipe` options are specified before the first stage, and options for each stage are specified immediately after the stage name.

A *stage* is a specific processing step in the pipeline, such as connecting to a BGP endpoint, filtering messages, or executing a command. You can think of it as a building block that performs a specific task in the overall message processing flow. In order to learn more about a specific stage, you can run `bgpipe <stage> -h`, for example:

```
$ bgpipe connect -h
Stage usage: connect [OPTIONS] ADDR

Description: connect to a BGP endpoint over TCP

Options:
      --timeout duration   connect timeout (0 means none) (default 1m0s)
      --closed duration    half-closed timeout (0 means none) (default 1s)
      --md5 string         TCP MD5 password

Common Options:
  -L, --left               operate in the L direction
  -R, --right              operate in the R direction
  -A, --args               consume all CLI arguments till --
  -W, --wait strings       wait for given event before starting
  -S, --stop strings       stop after given event is handled
  -N, --new string         which stage to send new messages to (default "next")
  -O, --of string          stage output filter (drop non-matching output)
```

As you can see, the `connect` stage has its own set of options, such as `--timeout`, `--closed`, and `--md5`, which are specific to establishing a BGP connection. The common options, such as `-L`, `-R`, etc. are available for all stages and control how the stage operates in the pipeline context.

By default, all stages operate in the *right* (`-R`) direction, meaning that they process BGP messages flowing from left to right. The direction controls which messages to capture for processing in a stage, and where to send new messages. However, if the last stage connects to a BGP endpoint, by default it will operate in the *left* (`-L`) direction, meaning it will send new messages to the left of the pipeline. Usually, the left-most and/or right-most stage is the one that connects to a BGP endpoint, while the other stages process messages in between. If you want bidirectional processing, use the `-L` and `-R` options together, ie. `-LR`.

## Reading MRT files

Let's demonstrate basic message processing by reading MRT files. MRT files are a standard format for storing BGP messages, and `bgpipe` can read them from a file or a URL. You can even stream MRT files directly from the [RIPE NCC RIS](https://ris.ripe.net/docs/mrt/) or [RouteViews](https://archive.routeviews.org/) archives.

Below is an example of reading a compressed MRT file from the RIPE NCC RIS archive, filtering it for a specific prefix, and printing the results to stdout:

```json
$ bgpipe \
    -- read --mrt https://data.ris.ripe.net/rrc01/latest-update.gz \
    -- grep 'prefix ~ 8.0.0.0/8' \
    -- stdout
2025-07-04 13:17:47 INF streaming https://data.ris.ripe.net/rrc01/latest-update.gz stage="[1] read"
["R",6826,"2025-07-04T13:05:19.000",74,"UPDATE",{"reach":["8.20.247.0/24","8.26.56.0/24","104.37.179.0/24","199.167.65.0/24"],"attrs":{"ORIGIN":{"flags":"T","value":"IGP"},"ASPATH":{"flags":"T","value":[8218,174,20473,23393]},"NEXTHOP":{"flags":"T","value":"5.57.80.210"},"MED":{"flags":"O","value":4},"COMMUNITY":{"flags":"OT","value":["8218:102","8218:20000","8218:20110"]}}},{"PEER_AS":"8218","PEER_IP":"5.57.80.210","LOCAL_AS":"12654","LOCAL_IP":"5.57.80.4"}]
["R",7431,"2025-07-04T13:05:21.000",77,"UPDATE",{"reach":["8.20.247.0/24","8.26.56.0/24","104.37.179.0/24","199.167.65.0/24"],"attrs":{"ORIGIN":{"flags":"T","value":"IGP"},"ASPATH":{"flags":"T","value":[8218,20473,23393]},"NEXTHOP":{"flags":"T","value":"5.57.80.210"},"MED":{"flags":"O","value":4},"COMMUNITY":{"flags":"OT","value":["8218:102","8218:20000","8218:20110"]},"OTC":{"flags":"OTP","value":"0x00001a79"}}},{"LOCAL_AS":"12654","LOCAL_IP":"5.57.80.4","PEER_AS":"8218","PEER_IP":"5.57.80.210"}]
// ...
```

In the above, the `read` stage streams the latest BGP updates from the `rrc01` RIPE RIS collector, uncompresses the data on the fly, and sends back to the pipeline for further processing. Next, the `grep` stage captures these messages, applies a BGP message filter (IP prefix must overlap with the `8.0.0.0/8` IPv4 prefix), and sends accepted messages to the next stage (non-matching traffic is dropped). Finally, the `stdout` stage converts the messages to JSON format and prints them to stdout.

`bgpipe` provides the `--explain` (short `-n`) debugging option that prints the pipeline as configured, but without actually running anything. For example:

```json
$ bgpipe -n \
    -- read --mrt https://data.ris.ripe.net/rrc01/latest-update.gz \
    -- grep 'prefix ~ 8.0.0.0/8' \
    -- stdout
--> MESSAGES FLOWING RIGHT -->
  [1] read --mrt https://data.ris.ripe.net/rrc01/latest-update.gz
      writes messages to pipeline inputs=1
  [2] grep prefix ~ 8.0.0.0/8
      reads messages from pipeline callbacks=1 types=[ALL]
  [3] stdout
      reads messages from pipeline callbacks=1 types=[ALL]

<-- MESSAGES FLOWING LEFT <--
  (none)
```

Last but not least, instead of putting the `stdout` stage explicitly in the pipeline, you can use the `--stdout` (short `-o`) option to `bgpipe`, in order to print BGP messages to stdout automatically. It will print all messages that make it to the very end of the left-hand side *and* right-hand side of the pipeline, ie. all messages that are not dropped by any stage.

```json
$ bgpipe -o \
    -- read --mrt https://data.ris.ripe.net/rrc01/latest-update.gz \
    -- grep 'prefix ~ 8.0.0.0/8'
...
```

## Connecting to a BGP speaker

Now that you know how to read MRT files, let's connect to a BGP speaker and process messages in real-time. You can use the `connect` stage to establish the TCP connection, and the `speaker` stage to open and maintain a BGP session.

We will use this opportunity to connect to one of [the BGP projects run by Łukasz Bromirski](https://lukasz.bromirski.net/projects/). The following command connects to the [BGP Blackholing with Flowspec endpoint](https://lukasz.bromirski.net/bgp-fs-blackholing/) and prints the conversation to stdout, which demonstrates that `bgpipe` supports [Flowspec](https://datatracker.ietf.org/doc/html/rfc8955):

```json
$ bgpipe -o \
    -- speaker --active --asn 65055 \
    -- connect 85.232.240.180
2025-07-11 10:47:20 INF dialing 85.232.240.180:179 stage="[2] connect"
2025-07-11 10:47:20 INF connection R_LOCAL = 192.168.200.202:59438 stage="[2] connect"
2025-07-11 10:47:20 INF connection R_REMOTE = 85.232.240.180:179 stage="[2] connect"
2025-07-11 10:47:20 INF connected 192.168.200.202:59438 -> 85.232.240.180:179 stage="[2] connect"
["R",1,"2025-07-11T08:47:20.650",-1,"OPEN",{"bgp":4,"asn":65055,"id":"0.0.0.1","hold":90,"caps":{"MP":["IPV4/UNICAST","IPV4/FLOWSPEC","IPV6/UNICAST","IPV6/FLOWSPEC"],"ROUTE_REFRESH":true,"EXTENDED_MESSAGE":true,"AS4":65055}},{}]
["L",1,"2025-07-11T08:47:22.659",56,"OPEN",{"bgp":4,"asn":65055,"id":"85.232.240.180","hold":7200,"caps":{"MP":["IPV4/FLOWSPEC"],"ROUTE_REFRESH":true,"EXTENDED_NEXTHOP":["IPV4/UNICAST/IPV6","IPV4/MULTICAST/IPV6","IPV4/MPLS_VPN/IPV6"],"AS4":65055,"PRE_ROUTE_REFRESH":true}},{}]
["L",2,"2025-07-11T08:47:22.659",0,"KEEPALIVE",null,{}]
["R",2,"2025-07-11T08:47:22.659",0,"KEEPALIVE",null,{}]
2025-07-11 10:47:22 INF negotiated session capabilities caps="{\"MP\":[\"IPV4/FLOWSPEC\"],\"ROUTE_REFRESH\":true,\"AS4\":65055}"
2025-07-11 10:47:22 INF event bgpfix/pipe.ESTABLISHED evseq=15 vals=[1752223642]
...
```

## Proxying BGP sessions

Finally, let's see how to use `bgpipe` to proxy BGP sessions. You can use the `listen` stage to accept incoming connections and the `connect` stage to forward BGP messages to another router. This allows you to create a transparent proxy that can filter, modify, or log BGP messages.

For example, let's use the [Vultr's BGP feature](https://docs.vultr.com/configuring-bgp-on-vultr), where you already have a local BIRD instance running on a VM, with the following configuration:

```
log syslog all;
router id 1.2.3.4;

protocol bgp vultr
{
  local as 123;
  source address 1.2.3.4;
  ipv4 {
    import none;
    export none;
  };
  graceful restart on;
  multihop 2;
  neighbor 169.254.169.254 as 64515;
  password "solarwinds123";
}
```

Let's say you'd like to see all UPDATEs that match a specific ASN `15169`. First, let's run a `bgpipe` proxy that listens on port `1790` and connects to the upstream router with TCP-MD5 when its client connects.

```json
$ bgpipe \
  -- connect --wait listen --md5 "solarwinds123" 169.254.169.254 \
  -- stdout -LR --if "as_path = 15169" \
  -- listen localhost:1790
2025-07-11 09:16:47 INF listening on 127.0.0.1:1790 stage="[3] listen"
```

Now let's reconfigure the BIRD instance to connect to `bgpipe` instead of the upstream router. Change the `neighbor` line in the BIRD configuration to point to `localhost:1790`:

```
// ...
protocol bgp vultr
{
  // ...
  neighbor 127.0.0.1 port 1790 as 64515;
  // password ""; // no password needed
}
```

Finally, restart your BIRD instance and you should see `bgpipe` reporting new connections, followed by JSON representations of BGP messages matching your filter:

```json
2025-07-11 11:23:45 INF connection R_LOCAL = 127.0.0.1:1790 stage="[3] listen"
2025-07-11 11:23:45 INF connection R_REMOTE = 1.2.3.4:36297 stage="[3] listen"
2025-07-11 11:23:45 INF connected 127.0.0.1:1790 -> 1.2.3.4:36297 stage="[3] listen"
2025-07-11 11:23:45 INF dialing 169.254.169.254:179 stage="[1] connect"
2025-07-11 11:23:45 INF connection L_LOCAL = 1.2.3.4:33514 stage="[1] connect"
2025-07-11 11:23:45 INF connection L_REMOTE = 169.254.169.254:179 stage="[1] connect"
2025-07-11 11:23:45 INF connected 1.2.3.4:33514 -> 169.254.169.254:179 stage="[1] connect"
2025-07-11 11:23:46 INF negotiated session capabilities caps="{\"MP\":[\"IPV4/UNICAST\"],\"ROUTE_REFRESH\":true,\"GRACEFUL_RESTART\":true,\"AS4\":64515,\"ENHANCED_ROUTE_REFRESH\":true,\"LLGR\":true}"
2025-07-11 11:23:46 INF event bgpfix/pipe.ESTABLISHED evseq=15 vals=[1752233026]
2025-07-11 11:23:49 INF event bgpfix/pipe.EOR evdir=L evseq=18
["R",243,"2025-07-11T11:23:50.860",1459,"UPDATE",{"reach":[...],"attrs":{"ORIGIN":{"flags":"T","value":"EGP"},"ASPATH":{"flags":"TX","value":[64515,65534,20473,15169,396982]},"NEXTHOP":{"flags":"T","value":"169.254.169.254"},"COMMUNITY":{"flags":"OT","value":["20473:300","20473:15169","64515:44"]},"LARGE_COMMUNITY":{"flags":"OT","value":["20473:300:15169"]}}},{}]
...
```

## Conclusion

For more practical pipelines and advanced use cases, check out the [examples](examples.md) page. It contains real-world bgpipe command lines for BGP monitoring, proxying, filtering, and more.

Happy bgpiping!
