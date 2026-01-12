package rpki

import (
	"context"
	"io"

	"github.com/bgpfix/bgpipe/core"
	"github.com/rs/zerolog"
)

// newTestRpki creates a properly initialized Rpki instance for testing
// Use this when tests involve logging (file parsing, etc.)
func newTestRpki() *Rpki {
	logger := zerolog.New(io.Discard)
	ctx := context.Background()
	s := &Rpki{
		StageBase: &core.StageBase{
			Logger: logger,
			Ctx:    ctx,
		},
	}
	s.nextFlush()
	return s
}

// newTestRpkiSimple creates a minimal Rpki for testing core logic without logging
// Use this for tests that don't call methods requiring StageBase (e.g., pure ROA cache operations)
func newTestRpkiSimple() *Rpki {
	s := &Rpki{}
	s.nextFlush()
	return s
}
