
# Examples

```bash
# connect to a BGP speaker, respond to OPEN, dump to JSON
bgpipe -o speaker 1.2.3.4

# JSON to BGP and back
cat input.json | bgpipe -io speaker 1.2.3.4 | tee output.json

# dump MRT updates to JSON
bgpipe read --mrt updates.20230301.0000.bz2 -- write output.json

# proxy a connection, print the conversation to stdout by default
# 1st stage: listen on TCP *:179 for new connection
# 2nd stage: wait for new connection and proxy it to 1.2.3.4, adding TCP-MD5
bgpipe -o \
	-- listen :179 \
	-- connect --wait listen --md5 solarwinds123 1.2.3.4

# a BGP speaker that streams an MRT file
# 1st stage: active BGP speaker for AS65055
# 2nd stage: MRT file reader, starting when the BGP session is established
# 3rd stage: listen on TCP *:179 for new connection
bgpipe \
  -- speaker --active --asn 65055 \
  -- read --mrt --wait ESTABLISHED updates.20230301.0000.bz2 \
  -- listen :179

# a BGP sed-in-the-middle proxy rewriting ASNs in OPEN messages
bgpipe \
  -- connect 1.2.3.4 \
  -- exec -LR --args sed -ure '/"OPEN"/{ s/65055/65001/g; s/57355/65055/g }' \
  -- connect 85.232.240.179

# filter prefix lengths and add max-prefix session limits
bgpipe --kill limit/session \
  -- connect 1.2.3.4 \
  -- limit -LR --ipv4 --min-length  8 --max-length 24 --session 1000000 \
  -- limit -LR --ipv6 --min-length 16 --max-length 48 --session 250000 \
  -- connect 5.6.7.8

# stream a log of BGP session in JSON to a remote websocket
bgpipe \
  -- connect 1.2.3.4 \
  -- websocket -LR --write wss://bgpfix.com/archive?user=demo \
  -- connect 85.232.240.179

# proxy a connection dropping non-IPv4 updates
bgpipe \
  -- connect 1.2.3.4 \
  -- grep -v --ipv4 \
  -- connect 85.232.240.179
```
