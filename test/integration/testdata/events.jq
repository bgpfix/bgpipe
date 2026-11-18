# per-prefix events: time peer prefix A|W aspath
select(.[3] == "UPDATE")
| .[2] as $t
| (.[5] // {} | "\(.COLLECTOR)|\(.PEER_IP)|\(.PEER_AS)") as $peer
| .[4] as $u
| ($u.attrs.ASPATH.value // [] | map(tostring) | join(",")) as $path
| ((($u.reach // []) + ($u.attrs.MP_REACH.value.prefixes // []))[] | "\($t) \($peer) \(.) A \($path)"),
  ((($u.unreach // []) + ($u.attrs.MP_UNREACH.value.prefixes // []))[] | "\($t) \($peer) \(.) W -")
