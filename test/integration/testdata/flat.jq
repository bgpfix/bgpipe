# flatten bgpipe JSON UPDATEs into per-prefix lines: dir peer prefix A|W attrs
select(.[3] == "UPDATE")
| .[0] as $dir
| (.[5] // {} | to_entries | sort_by(.key) | map("\(.key)=\(.value)") | join(",")) as $peer
| .[4] as $u
| ($u.attrs // {} | del(.MP_REACH, .MP_UNREACH) | tojson) as $attrs
| ($u.attrs.MP_REACH.value // {}) as $mpr
| (
    ($u.reach // [])[] | "\($dir) \($peer) \(.) A \($attrs)"
  ),
  (
    ($mpr.prefixes // [])[] | "\($dir) \($peer) \(.) A \($attrs) nh=\($mpr.nexthop) ll=\($mpr.link_local)"
  ),
  (
    (($u.unreach // []) + ($u.attrs.MP_UNREACH.value.prefixes // []))[] | "\($dir) \($peer) \(.) W"
  )
