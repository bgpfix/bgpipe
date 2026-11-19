package rvlive

import (
	"testing"

	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/stretchr/testify/require"
)

// a 2-byte UPDATE for 1.1.1.0/24 with AS_PATH [6447 23456] and AS4_PATH [6447 200000]
var as2Update = []byte{
	0, 0, // no withdrawn
	0, 33, // attrs length
	0x40, 1, 1, 0, // ORIGIN IGP
	0x40, 2, 6, 2, 2, 0x19, 0x2f, 0x5b, 0xa0, // AS_PATH [6447 23456]
	0x40, 3, 4, 192, 0, 2, 1, // NEXT_HOP
	0xc0, 17, 10, 2, 2, 0, 0, 0x19, 0x2f, 0, 3, 0x0d, 0x40, // AS4_PATH [6447 200000]
	24, 1, 1, 1, // 1.1.1.0/24
}

// fixPath on a complete AS4_PATH must leave a proper AS_PATH, and the message re-usable
func TestFixPathAS4(t *testing.T) {
	s := &RvLive{StageBase: &core.StageBase{P: pipe.NewPipe(t.Context())}}

	m := msg.NewMsg()
	m.Type = msg.UPDATE
	m.Data = append([]byte(nil), as2Update...)
	s.fixPath(m, 200000)

	require.Equal(t, "[200000]", m.Update.AsPath().String())
	require.False(t, m.Update.Attrs.Has(attrs.ATTR_AS4PATH))
	require.Equal(t, attrs.ATTR_ASPATH, m.Update.Attrs.Get(attrs.ATTR_ASPATH).Code())

	m.Reset()
	m.Type = msg.UPDATE
	m.Data = append([]byte(nil), as2Update...)
	require.NoError(t, m.Parse(caps.Caps{}))
	require.Equal(t, "[6447,200000]", m.Update.AsPath().String())
}
