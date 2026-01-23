package rvlive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// rvState holds offset state for persistence
type rvState struct {
	Version   int                        `json:"version"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Offsets   map[string]map[int32]int64 `json:"offsets"` // topic -> partition -> offset
}

func (s *RvLive) stateSaver(done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-s.Ctx.Done():
			return
		case <-ticker.C:
			s.stateMu.Lock()
			if s.stateDirty {
				s.stateDirty = false
				s.stateMu.Unlock()
				if err := s.saveState(); err != nil {
					s.Warn().Err(err).Msg("failed to save state")
				}
			} else {
				s.stateMu.Unlock()
			}
		}
	}
}

func (s *RvLive) updateOffset(topic string, partition int32, offset int64) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.state.Offsets[topic] == nil {
		s.state.Offsets[topic] = make(map[int32]int64)
	}
	s.state.Offsets[topic][partition] = offset
	s.stateDirty = true
}

func (s *RvLive) loadState() (*rvState, error) {
	data, err := os.ReadFile(s.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &rvState{Version: 1, Offsets: make(map[string]map[int32]int64)}, nil
		}
		return nil, err
	}

	var state rvState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	if state.Offsets == nil {
		state.Offsets = make(map[string]map[int32]int64)
	}

	return &state, nil
}

func (s *RvLive) saveState() error {
	s.stateMu.Lock()
	s.state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(s.state, "", "  ")
	s.stateMu.Unlock()

	if err != nil {
		return err
	}

	// Atomic write: temp file + rename
	dir := filepath.Dir(s.stateFile)
	tmp, err := os.CreateTemp(dir, ".rv-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, s.stateFile); err != nil {
		os.Remove(tmpName)
		return err
	}

	s.Debug().Str("file", s.stateFile).Msg("state saved")
	return nil
}
