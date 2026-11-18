# split bgpipe JSON UPDATEs into one UPDATE per prefix (like RouteViews over Kafka)
select(.[3] == "UPDATE") | . as $m | .[4] as $u
| ( ($u.reach // [])[] as $p
	| $m | .[4] = ($u | del(.unreach, .attrs.MP_REACH, .attrs.MP_UNREACH) | .reach = [$p]) ),
  ( ($u.unreach // [])[] as $p
	| $m | .[4] = {unreach: [$p]} ),
  ( ($u.attrs.MP_REACH.value.prefixes // [])[] as $p
	| $m | .[4] = ($u | del(.reach, .unreach, .attrs.MP_UNREACH) | .attrs.MP_REACH.value.prefixes = [$p]) ),
  ( ($u.attrs.MP_UNREACH.value.prefixes // [])[] as $p
	| $m | .[4] = {attrs: {MP_UNREACH: ($u.attrs.MP_UNREACH | .value.prefixes = [$p])}} )
